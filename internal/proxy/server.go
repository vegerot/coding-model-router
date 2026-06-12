package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	decision, err := ParseKnob(model, r.Header.Get("X-Pareto-P"), s.defaultP)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	plan := engine.Plan{}
	if decision.Route {
		key := stickySessionKey(r, payload, decision.P)
		if cached, ok := s.stickies.get(key, decision.P); ok {
			plan = cached
		} else {
			plan, err = engine.Select(s.snapshot, decision.P, engine.Options{})
			if err != nil {
				writeError(w, http.StatusBadGateway, "model selection failed: "+err.Error())
				return
			}
			s.stickies.set(key, decision.P, plan)
		}
		payload["model"] = plan.Primary.OpenRouterID
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

	if s.logger != nil {
		logRequest(s.logger, r, decision.P, plan, resp)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	streamBody(w, resp.Body)
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
	FallbackHops int           `json:"fallback_hops"`
	Status       int           `json:"status"`
	Attempts     []attemptInfo `json:"attempts,omitempty"`
	Output       string        `json:"output,omitempty"`
}

type attemptInfo struct {
	Model  string `json:"model"`
	Status int    `json:"status"`
}

func logRequest(w io.Writer, r *http.Request, p float64, plan engine.Plan, resp *http.Response) {
	entry := logEntry{
		SessionID:    stickySessionID(r),
		P:            p,
		Model:        plan.Primary.OpenRouterID,
		Provider:     plan.Primary.Provider,
		FallbackHops: len(plan.Fallbacks),
		Status:       resp.StatusCode,
		Attempts: []attemptInfo{{
			Model:  plan.Primary.OpenRouterID,
			Status: resp.StatusCode,
		}},
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, err := io.ReadAll(resp.Body)
		if err == nil {
			entry.Output = string(data)
			resp.Body = io.NopCloser(bytes.NewReader(data))
		}
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = w.Write(append(data, '\n'))
}

func streamBody(w http.ResponseWriter, body io.Reader) {
	flusher, _ := w.(http.Flusher)
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

func stickySessionKey(r *http.Request, payload map[string]any, p float64) string {
	if v := strings.TrimSpace(r.Header.Get("X-Session-Id")); v != "" {
		return fmt.Sprintf("session:%s:p:%g", v, p)
	}
	fp := conversationFingerprint(payload)
	if fp == "" {
		return ""
	}
	return fmt.Sprintf("fingerprint:%s:p:%g", fp, p)
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
