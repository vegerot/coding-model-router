package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/vegerot/coding-model-router/internal/engine"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

const (
	defaultUpstreamBase = "https://openrouter.ai/api/v1"
	chatCompletionsPath = "/v1/chat/completions"
)

// Config configures a Server. Snapshot is required and must already be mapped
// (every candidate carries an OpenRouterID); see mapping.MappedSnapshot.
type Config struct {
	Snapshot      *snapshot.Snapshot // mapped candidate set (required)
	DefaultP      float64            // floor for a bare "pareto" model name
	OpenRouterKey string             // injected when the client sends no Authorization
	UpstreamBase  string             // default "https://openrouter.ai/api/v1"
	Client        *http.Client       // nil → http.DefaultClient
	Referer       string             // OpenRouter attribution: HTTP-Referer
	Title         string             // OpenRouter attribution: X-Title
}

// Server is the OpenAI-compatible proxy handler.
type Server struct {
	snapshot      *snapshot.Snapshot
	defaultP      float64
	openRouterKey string
	upstreamBase  string
	client        *http.Client
	referer       string
	title         string
}

// NewServer validates the config and returns a ready Server.
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
	}, nil
}

// ServeHTTP routes POST /v1/chat/completions: parse the knob, (optionally)
// select a model and rewrite the model field, then forward to OpenRouter.
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

	outBody := body
	if decision.Route {
		plan, err := engine.Select(s.snapshot, decision.P, engine.Options{})
		if err != nil {
			writeError(w, http.StatusBadGateway, "model selection failed: "+err.Error())
			return
		}
		payload["model"] = plan.Primary.OpenRouterID
		outBody, err = json.Marshal(payload)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "re-encode request: "+err.Error())
			return
		}
	}

	s.forward(w, r, outBody)
}

func (s *Server) forward(w http.ResponseWriter, r *http.Request, body []byte) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.upstreamBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "build upstream request: "+err.Error())
		return
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

	resp, err := s.client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	streamBody(w, resp.Body)
}

// streamBody copies the upstream body to the client, flushing after each read so
// SSE chunks reach the client as they arrive.
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
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "router_error",
		},
	})
}
