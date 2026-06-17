package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vegerot/coding-model-router/internal/engine"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

const (
	defaultUpstreamBase = "https://openrouter.ai/api/v1"
	chatCompletionsPath = "/v1/chat/completions"
	defaultStickyTTL    = 5 * time.Minute
	maxFallbackModels   = 3
)

type Config struct {
	Snapshot      *snapshot.Snapshot
	DefaultP      float64
	OpenRouterKey string
	UpstreamBase  string
	Client        *http.Client
	Referer       string
	Title         string
	Logger        io.Writer
}

type Server struct {
	snapshot      *snapshot.Snapshot
	defaultP      float64
	openRouterKey string
	upstreamBase  string
	client        *http.Client
	referer       string
	title         string
	logger        io.Writer
	stickies      *stickyStore
}

func NewServer(cfg Config) (*Server, error) {
	if cfg.Snapshot == nil {
		return nil, errors.New("proxy: nil snapshot")
	}
	base := cfg.UpstreamBase
	if base == "" {
		base = defaultUpstreamBase
	}
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &Server{
		snapshot:      cfg.Snapshot,
		defaultP:      cfg.DefaultP,
		openRouterKey: cfg.OpenRouterKey,
		upstreamBase:  strings.TrimRight(base, "/"),
		client:        client,
		referer:       cfg.Referer,
		title:         cfg.Title,
		logger:        cfg.Logger,
		stickies:      newStickyStore(defaultStickyTTL),
	}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != chatCompletionsPath || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not found: only POST "+chatCompletionsPath+" is served")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read request body: "+err.Error())
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	model, _ := payload["model"].(string)

	usageMap, ok := payload["usage"].(map[string]any)
	if !ok {
		usageMap = map[string]any{}
		payload["usage"] = usageMap
	}
	usageMap["include"] = true

	decision, err := ParseKnob(model, r.Header.Get("X-Pareto-P"), s.defaultP)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	plan := engine.Plan{}
	var sentFallbacks []string
	if decision.Route {
		plan, err = s.selectPlan(r, payload, decision.P)
		if err != nil {
			writeError(w, http.StatusBadGateway, "model selection failed: "+err.Error())
			return
		}
		payload["model"] = plan.Primary.OpenRouterID

		sentFallbacks = s.fallbackModelIDs(plan)
		if len(sentFallbacks) > 0 {
			payload["models"] = sentFallbacks
		}

		body, err = json.Marshal(payload)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "re-encode request: "+err.Error())
			return
		}
	}

	resp, err := s.forward(r, body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)

	// Stream live to the client while sniffing the served model (and, on error, the
	// full body) so logging reflects what OpenRouter actually served — including a
	// native fallback — without buffering the whole stream.
	sniff := &servedModelSniffer{logErrorBody: resp.StatusCode < 200 || resp.StatusCode > 299}
	streamBody(w, io.TeeReader(resp.Body, sniff))

	if s.logger != nil {
		s.logRequest(s.logger, r, decision.P, model, plan, sentFallbacks, resp.StatusCode, sniff.errorBody(), sniff.servedModel())
	}
}

func (s *Server) selectPlan(r *http.Request, payload map[string]any, p float64) (engine.Plan, error) {
	key, hasStickyKey := stickySessionKey(r, payload, p)
	if hasStickyKey {
		if cached, ok := s.stickies.get(key, p); ok {
			return cached, nil
		}
	}
	plan, err := engine.Select(s.snapshot, p, engine.Options{})
	if err != nil {
		return engine.Plan{}, err
	}
	if hasStickyKey {
		s.stickies.set(key, p, plan)
	}
	return plan, nil
}

func (s *Server) fallbackModelIDs(plan engine.Plan) []string {
	fallbacks := make([]string, 0, maxFallbackModels)
	seen := map[string]bool{plan.Primary.OpenRouterID: true}

	for _, c := range plan.Fallbacks {
		if c.OpenRouterID == "" || seen[c.OpenRouterID] {
			continue
		}
		fallbacks = append(fallbacks, c.OpenRouterID)
		seen[c.OpenRouterID] = true
		if len(fallbacks) == maxFallbackModels {
			return fallbacks
		}
	}

	extra := make([]snapshot.Candidate, 0, len(s.snapshot.Candidates))
	for _, c := range s.snapshot.Candidates {
		if c.OpenRouterID == "" || seen[c.OpenRouterID] {
			continue
		}
		extra = append(extra, c)
	}
	sort.SliceStable(extra, func(i, j int) bool {
		if extra[i].Quality != extra[j].Quality {
			return extra[i].Quality > extra[j].Quality
		}
		if extra[i].BlendedPricePer1M != extra[j].BlendedPricePer1M {
			return extra[i].BlendedPricePer1M < extra[j].BlendedPricePer1M
		}
		return extra[i].Slug < extra[j].Slug
	})
	for _, c := range extra {
		fallbacks = append(fallbacks, c.OpenRouterID)
		if len(fallbacks) == maxFallbackModels {
			break
		}
	}
	return fallbacks
}

func (s *Server) forward(r *http.Request, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.upstreamBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth := r.Header.Get("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	} else if s.openRouterKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.openRouterKey)
	}
	if s.referer != "" {
		req.Header.Set("HTTP-Referer", s.referer)
	}
	if s.title != "" {
		req.Header.Set("X-Title", s.title)
	}
	if sessionID := strings.TrimSpace(r.Header.Get("X-Session-Id")); sessionID != "" {
		req.Header.Set("X-Session-Id", sessionID)
	}
	return s.client.Do(req)
}

type logEntry struct {
	SessionID    string        `json:"session_id,omitempty"`
	P            float64       `json:"p"`
	Model        string        `json:"model,omitempty"`
	Provider     string        `json:"provider,omitempty"`
	FallbackHops int           `json:"-"`
	Status       int           `json:"-"`
	Attempts     []attemptInfo `json:"-"`
	Output       string        `json:"output,omitempty"`
}

func (e logEntry) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if e.SessionID != "" {
		m["session_id"] = e.SessionID
	}
	m["p"] = e.P
	if e.Model != "" {
		m["model"] = e.Model
	}
	if e.Provider != "" {
		m["provider"] = e.Provider
	}
	if e.FallbackHops != 0 {
		m["fallback_hops"] = e.FallbackHops
	}
	if e.Status != 0 && e.Status != 200 {
		m["status"] = e.Status
	}
	if len(e.Attempts) > 1 {
		m["attempts"] = e.Attempts
	}
	if e.Output != "" {
		m["output"] = e.Output
	}
	return json.Marshal(m)
}

type attemptInfo struct {
	Model  string `json:"model"`
	Status int    `json:"status"`
}

func (s *Server) logRequest(w io.Writer, r *http.Request, p float64, requestModel string, plan engine.Plan, sentFallbacks []string, status int, respBody []byte, servedModel string) {
	model := requestModel
	provider := ""
	fallbackHops := 0
	attempts := []attemptInfo(nil)
	if plan.Primary.OpenRouterID != "" {
		// A routed request. The model we requested as primary is the first attempt;
		// OpenRouter may have served a native fallback instead. Prefer the model
		// OpenRouter actually reported serving.
		model = plan.Primary.OpenRouterID
		provider = plan.Primary.Provider

		attemptChain := append([]string{plan.Primary.OpenRouterID}, sentFallbacks...)
		if servedModel != "" {
			// Always trust the model OpenRouter reported serving. Match it back to the
			// attempt chain to count fallback hops; if it isn't one we recognize, still
			// log it honestly with 0 hops rather than claiming the requested primary.
			model = servedModel
			if hop := matchServedModel(attemptChain, servedModel); hop >= 0 {
				fallbackHops = hop
			}
			provider = s.providerForOpenRouterID(servedModel)
		}

		attempts = make([]attemptInfo, 0, fallbackHops+1)
		for i := 0; i < fallbackHops && i < len(attemptChain); i++ {
			// Everything before the served model is presumed to have failed over.
			attempts = append(attempts, attemptInfo{Model: attemptChain[i], Status: http.StatusServiceUnavailable})
		}
		attempts = append(attempts, attemptInfo{Model: model, Status: status})
	}
	entry := logEntry{
		SessionID:    stickySessionID(r),
		P:            p,
		Model:        model,
		Provider:     provider,
		FallbackHops: fallbackHops,
		Status:       status,
		Attempts:     attempts,
	}

	if status < 200 || status > 299 {
		entry.Output = string(respBody)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = w.Write(append(data, '\n'))
}

func (s *Server) providerForOpenRouterID(id string) string {
	for _, c := range s.snapshot.Candidates {
		if c.OpenRouterID == id {
			return c.Provider
		}
	}
	return ""
}

// matchServedModel finds which attempt OpenRouter served. OpenRouter often
// appends a date/variant suffix to the served model (e.g. our "openai/gpt-5.5"
// comes back as "openai/gpt-5.5-20260423"), so an exact match is tried first and
// then a `<id>-` prefix match.
func matchServedModel(ids []string, served string) int {
	for i, id := range ids {
		if id == served {
			return i
		}
	}
	for i, id := range ids {
		if id != "" && strings.HasPrefix(served, id+"-") {
			return i
		}
	}
	return -1
}

// maxSniffBytes bounds how much of a streamed response we buffer to detect the
// served model. The model field appears in the first SSE chunk / JSON object, so
// a small cap is plenty while keeping memory bounded for long streams.
const maxSniffBytes = 64 * 1024

// servedModelSniffer is an io.Writer placed on the tee of the upstream stream. It
// buffers up to maxSniffBytes to extract the served model once, and (for error
// responses) retains the full body for logging.
type servedModelSniffer struct {
	logErrorBody bool
	buf          bytes.Buffer
	full         bytes.Buffer
	found        string
	done         bool
}

func (s *servedModelSniffer) Write(p []byte) (int, error) {
	if s.logErrorBody {
		s.full.Write(p)
	}
	if !s.done {
		remaining := maxSniffBytes - s.buf.Len()
		if remaining > 0 {
			if len(p) < remaining {
				remaining = len(p)
			}
			s.buf.Write(p[:remaining])
		}
		if m := servedModelFromResponse(s.buf.Bytes()); m != "" {
			s.found = m
			s.done = true
		} else if s.buf.Len() >= maxSniffBytes {
			s.done = true
		}
	}
	return len(p), nil
}

func (s *servedModelSniffer) servedModel() string { return s.found }

func (s *servedModelSniffer) errorBody() []byte {
	if !s.logErrorBody {
		return nil
	}
	return s.full.Bytes()
}

// servedModelFromResponse extracts the model OpenRouter reported serving, from
// either a JSON completion body or the first SSE data chunk.
func servedModelFromResponse(body []byte) string {
	var obj struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &obj); err == nil && obj.Model != "" {
		return obj.Model
	}
	// SSE: scan each `data:` line for the first chunk carrying a model field.
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		payload, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok {
			continue
		}
		payload = bytes.TrimSpace(payload)
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		if err := json.Unmarshal(payload, &obj); err == nil && obj.Model != "" {
			return obj.Model
		}
	}
	return ""
}

func streamBody(w http.ResponseWriter, body io.Reader) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("proxy: response writer does not implement http.Flusher")
	}
	buf := make([]byte, 4096)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			return
		}
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": msg, "type": "router_error"}})
}

type stickyEntry struct {
	p       float64
	plan    engine.Plan
	expires time.Time
}
type stickyStore struct {
	mu   sync.Mutex
	ttl  time.Duration
	data map[string]stickyEntry
}

func newStickyStore(ttl time.Duration) *stickyStore {
	return &stickyStore{ttl: ttl, data: make(map[string]stickyEntry)}
}

func (s *stickyStore) get(key string, p float64) (engine.Plan, bool) {
	if key == "" {
		return engine.Plan{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data[key]
	if !ok || time.Now().After(entry.expires) || entry.p != p {
		delete(s.data, key)
		return engine.Plan{}, false
	}
	entry.expires = time.Now().Add(s.ttl)
	s.data[key] = entry
	return entry.plan, true
}

func (s *stickyStore) set(key string, p float64, plan engine.Plan) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = stickyEntry{p: p, plan: plan, expires: time.Now().Add(s.ttl)}
}

func stickySessionID(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Session-Id")); v != "" {
		return v
	}
	return ""
}

func stickySessionKey(r *http.Request, payload map[string]any, p float64) (string, bool) {
	if v := strings.TrimSpace(r.Header.Get("X-Session-Id")); v != "" {
		return fmt.Sprintf("session:%s:p:%g", v, p), true
	}
	fp := conversationFingerprint(payload)
	if fp == "" {
		return "", false
	}
	return fmt.Sprintf("fingerprint:%s:p:%g", fp, p), true
}

func conversationFingerprint(payload map[string]any) string {
	messages, ok := payload["messages"].([]any)
	if !ok {
		return ""
	}
	var systemMsg, userMsg string
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		switch {
		case role == "system" && systemMsg == "":
			systemMsg = content
		case role == "user" && userMsg == "":
			userMsg = content
		}
		if systemMsg != "" && userMsg != "" {
			break
		}
	}
	if systemMsg == "" && userMsg == "" {
		return ""
	}
	h := sha256.Sum256([]byte(systemMsg + "\x00" + userMsg))
	return hex.EncodeToString(h[:])
}
