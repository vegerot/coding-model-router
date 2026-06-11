package proxy_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vegerot/coding-model-router/internal/proxy"
	"github.com/vegerot/coding-model-router/internal/snapshot"
)

func mappedSnapshot() *snapshot.Snapshot {
	return &snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		Attribution:   snapshot.Attribution,
		Candidates: []snapshot.Candidate{
			{
				Slug:              "cheap",
				OpenRouterID:      "test/cheap",
				Name:              "Cheap",
				Quality:           30,
				InputPricePer1M:   0.1,
				OutputPricePer1M:  0.1,
				BlendedPricePer1M: 0.1,
				Provider:          "test",
			},
			{
				Slug:              "mid",
				OpenRouterID:      "test/mid",
				Name:              "Mid",
				Quality:           50,
				InputPricePer1M:   5,
				OutputPricePer1M:  5,
				BlendedPricePer1M: 5,
				Provider:          "test",
			},
			{
				Slug:              "top",
				OpenRouterID:      "test/top",
				Name:              "Top",
				Quality:           90,
				InputPricePer1M:   20,
				OutputPricePer1M:  20,
				BlendedPricePer1M: 20,
				Provider:          "test",
			},
		},
	}
}

type capture struct {
	calls int
	model string
	auth  string
	head  string
}

func fakeUpstream(t *testing.T, cap *capture, respond func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.calls++
		cap.auth = r.Header.Get("Authorization")
		cap.head = r.Header.Get("X-Session-Id")
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		if s, ok := m["model"].(string); ok {
			cap.model = s
		}
		respond(w, r)
	}))
}

func newProxy(t *testing.T, snap *snapshot.Snapshot, upstream string, opts ...func(*proxy.Config)) *httptest.Server {
	t.Helper()
	cfg := proxy.Config{Snapshot: snap, DefaultP: 0.67, OpenRouterKey: "test-key", UpstreamBase: upstream}
	for _, opt := range opts {
		opt(&cfg)
	}
	srv, err := proxy.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return httptest.NewServer(srv)
}

func post(t *testing.T, url, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestServeRoutesAndRewritesModel(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"x","model":"test/top"}`)
	})
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.8","messages":[]}`, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cap.model != "test/top" {
		t.Errorf("upstream model = %q, want test/top", cap.model)
	}
	if cap.auth != "Bearer test-key" {
		t.Errorf("upstream auth = %q, want injected Bearer test-key", cap.auth)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "test/top") {
		t.Errorf("response body not relayed: %s", body)
	}
}

func TestServeSessionStickinessWithSessionID(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.0","messages":[]}`, map[string]string{"X-Session-Id": "abc"})
	resp.Body.Close()
	if cap.model != "test/cheap" {
		t.Fatalf("first routing model = %q, want test/cheap", cap.model)
	}

	resp = post(t, px.URL, `{"model":"pareto@0.0","messages":[]}`, map[string]string{"X-Session-Id": "abc"})
	resp.Body.Close()
	if cap.calls != 2 {
		t.Fatalf("upstream calls = %d, want 2", cap.calls)
	}
	if cap.model != "test/cheap" {
		t.Fatalf("sticky model = %q, want test/cheap", cap.model)
	}
}

func TestServeSessionStickinessIncludesP(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.0","messages":[]}`, map[string]string{"X-Session-Id": "abc"})
	resp.Body.Close()
	if cap.model != "test/cheap" {
		t.Fatalf("first routing model = %q, want test/cheap", cap.model)
	}

	resp = post(t, px.URL, `{"model":"pareto@1.0","messages":[]}`, map[string]string{"X-Session-Id": "abc"})
	resp.Body.Close()
	if cap.model != "test/top" {
		t.Fatalf("updated p routing model = %q, want test/top", cap.model)
	}
}

func TestServeSessionStickinessWithFingerprint(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	body := `{"model":"pareto@0.0","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hello"}]}`
	resp := post(t, px.URL, body, nil)
	resp.Body.Close()
	if cap.model != "test/cheap" {
		t.Fatalf("fingerprint routing model = %q, want test/cheap", cap.model)
	}

	resp = post(t, px.URL, body, nil)
	resp.Body.Close()
	if cap.model != "test/cheap" {
		t.Fatalf("fingerprint sticky model = %q, want test/cheap", cap.model)
	}
}

func TestServeForwardsClientAuthorization(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.8","messages":[]}`, map[string]string{
		"Authorization": "Bearer client-key",
	})
	resp.Body.Close()
	if cap.auth != "Bearer client-key" {
		t.Errorf("upstream auth = %q, want client-supplied Bearer client-key", cap.auth)
	}
}

func TestServePassesThroughNonParetoModel(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"openai/gpt-4o","messages":[]}`, nil)
	resp.Body.Close()
	if cap.model != "openai/gpt-4o" {
		t.Errorf("upstream model = %q, want unchanged openai/gpt-4o", cap.model)
	}
}

func TestServeRejectsMalformedKnobWithoutCallingUpstream(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@2","messages":[]}`, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	if cap.calls != 0 {
		t.Errorf("upstream called %d times, want 0 on malformed knob", cap.calls)
	}
}

func TestServeStreamsSSE(t *testing.T) {
	chunks := []string{"data: {\"a\":1}\n\n", "data: {\"b\":2}\n\n", "data: [DONE]\n\n"}
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, c := range chunks {
			io.WriteString(w, c)
			if fl != nil {
				fl.Flush()
			}
		}
	})
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.8","stream":true,"messages":[]}`, nil)
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	got := string(body)
	for _, c := range chunks {
		if !strings.Contains(got, strings.TrimSpace(c)) {
			t.Errorf("relayed stream missing %q\n---\n%s", c, got)
		}
	}
}

func TestServeReturns502WhenNoCandidateQualifies(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	defer up.Close()
	empty := &snapshot.Snapshot{SchemaVersion: snapshot.SchemaVersion, Attribution: snapshot.Attribution}
	px := newProxy(t, empty, up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.5","messages":[]}`, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	if cap.calls != 0 {
		t.Errorf("upstream called %d times, want 0 when selection fails", cap.calls)
	}
}

func TestNewServerRejectsNilSnapshot(t *testing.T) {
	if _, err := proxy.NewServer(proxy.Config{}); err == nil {
		t.Error("expected error for nil snapshot")
	}
}

func TestServerLogsStructuredRequest(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	defer up.Close()
	var log bytes.Buffer
	px := newProxy(t, mappedSnapshot(), up.URL, func(cfg *proxy.Config) { cfg.Logger = &log })
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.8","messages":[]}`, map[string]string{"X-Session-Id": "sess-1"})
	resp.Body.Close()
	if !strings.Contains(log.String(), `"session_id":"sess-1"`) {
		t.Fatalf("log missing session_id: %s", log.String())
	}
	if !strings.Contains(log.String(), `"p":0.8`) {
		t.Fatalf("log missing p: %s", log.String())
	}
}
