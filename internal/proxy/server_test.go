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

func mappedSnapshotMany() *snapshot.Snapshot {
	return &snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		Attribution:   snapshot.Attribution,
		Candidates: []snapshot.Candidate{
			{Slug: "cheap", OpenRouterID: "test/cheap", Name: "Cheap", Quality: 30, InputPricePer1M: 1, OutputPricePer1M: 1, BlendedPricePer1M: 1, Provider: "test"},
			{Slug: "mid", OpenRouterID: "test/mid", Name: "Mid", Quality: 50, InputPricePer1M: 2, OutputPricePer1M: 2, BlendedPricePer1M: 2, Provider: "test"},
			{Slug: "high", OpenRouterID: "test/high", Name: "High", Quality: 70, InputPricePer1M: 3, OutputPricePer1M: 3, BlendedPricePer1M: 3, Provider: "test"},
			{Slug: "higher", OpenRouterID: "test/higher", Name: "Higher", Quality: 80, InputPricePer1M: 4, OutputPricePer1M: 4, BlendedPricePer1M: 4, Provider: "test"},
			{Slug: "top", OpenRouterID: "test/top", Name: "Top", Quality: 90, InputPricePer1M: 5, OutputPricePer1M: 5, BlendedPricePer1M: 5, Provider: "test"},
		},
	}
}

type capture struct {
	calls        int
	model        string
	auth         string
	head         string
	usageInclude bool
	models       []string
}

type upstreamPayload struct {
	Model  string   `json:"model"`
	Models []string `json:"models"`
	Usage  struct {
		Include bool `json:"include"`
	} `json:"usage"`
}

func fakeUpstream(t *testing.T, cap *capture, respond func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.calls++
		cap.auth = r.Header.Get("Authorization")
		cap.head = r.Header.Get("X-Session-Id")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var m upstreamPayload
		_ = json.Unmarshal(body, &m)
		cap.model = m.Model
		cap.models = m.Models
		cap.usageInclude = m.Usage.Include
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "test/top") {
		t.Errorf("response body not relayed: %s", body)
	}
}

func TestServeIncludsModelsFallbackArray(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"x","model":"test/cheap"}`)
	})
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.0","messages":[]}`, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cap.model != "test/cheap" {
		t.Errorf("upstream model = %q, want test/cheap", cap.model)
	}
	if len(cap.models) != 2 || cap.models[0] != "test/mid" || cap.models[1] != "test/top" {
		t.Errorf("upstream models fallback array = %v, want [test/mid test/top]", cap.models)
	}
}

func TestServeAddsFallbacksForP1FromLowerQualityModels(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"x","model":"test/top"}`)
	})
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@1.0","messages":[]}`, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cap.model != "test/top" {
		t.Fatalf("upstream model = %q, want test/top", cap.model)
	}
	if len(cap.models) != 2 || cap.models[0] != "test/mid" || cap.models[1] != "test/cheap" {
		t.Fatalf("upstream models fallback array = %v, want [test/mid test/cheap]", cap.models)
	}
}

func TestServeLimitsModelsFallbackArrayToThree(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"x","model":"test/cheap"}`)
	})
	defer up.Close()
	px := newProxy(t, mappedSnapshotMany(), up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.0","messages":[]}`, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(cap.models) != 3 {
		t.Fatalf("len(models) = %d, want 3 (%v)", len(cap.models), cap.models)
	}
	want := []string{"test/mid", "test/high", "test/higher"}
	for i := range want {
		if cap.models[i] != want[i] {
			t.Fatalf("models[%d] = %q, want %q (all: %v)", i, cap.models[i], want[i], cap.models)
		}
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
		fl, ok := w.(http.Flusher)
		if !ok {
			panic("test response writer must implement http.Flusher")
		}
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

func TestServerLogsServedModelFromSSEStreamWithoutBuffering(t *testing.T) {
	released := make(chan struct{})
	firstChunkSent := make(chan struct{})
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl := w.(http.Flusher)
		io.WriteString(w, "data: {\"model\":\"test/mid\",\"choices\":[]}\n\n")
		fl.Flush()
		close(firstChunkSent)
		<-released // hold the stream open to prove relay is incremental, not buffered
		io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
	})
	defer up.Close()
	var log bytes.Buffer
	px := newProxy(t, mappedSnapshot(), up.URL, func(cfg *proxy.Config) { cfg.Logger = &log })
	defer px.Close()

	type result struct {
		body string
	}
	done := make(chan result, 1)
	go func() {
		resp := post(t, px.URL, `{"model":"pareto@0.0","stream":true,"messages":[]}`, nil)
		defer resp.Body.Close()
		buf := make([]byte, 1024)
		// Read the first relayed chunk while upstream still holds the stream open.
		n, _ := resp.Body.Read(buf)
		first := string(buf[:n])
		if !strings.Contains(first, "test/mid") {
			t.Errorf("did not receive first chunk incrementally, got %q", first)
		}
		close(released)
		rest, _ := io.ReadAll(resp.Body)
		done <- result{body: first + string(rest)}
	}()

	<-firstChunkSent
	res := <-done
	if !strings.Contains(res.body, "[DONE]") {
		t.Errorf("relayed stream missing [DONE]: %q", res.body)
	}

	var entry struct {
		Model        string `json:"model"`
		FallbackHops int    `json:"fallback_hops"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(log.Bytes()), &entry); err != nil {
		t.Fatalf("decode log: %v\n%s", err, log.String())
	}
	if entry.Model != "test/mid" {
		t.Errorf("logged model = %q, want test/mid from SSE stream", entry.Model)
	}
	if entry.FallbackHops != 1 {
		t.Errorf("logged fallback_hops = %d, want 1", entry.FallbackHops)
	}
}

func TestServeRoutesMultimodalArrayContent(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	body := `{"model":"pareto@0.0","messages":[{"role":"system","content":"sys"},{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"data:image/png;base64,xx"}}]}]}`
	resp := post(t, px.URL, body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cap.model != "test/cheap" {
		t.Errorf("upstream model = %q, want test/cheap", cap.model)
	}

	resp2 := post(t, px.URL, body, nil)
	resp2.Body.Close()
	if cap.model != "test/cheap" {
		t.Errorf("sticky model = %q, want test/cheap", cap.model)
	}
}

func TestServeRequestsOpenRouterUsageCost(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	defer up.Close()
	px := newProxy(t, mappedSnapshot(), up.URL)
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.8","messages":[]}`, nil)
	resp.Body.Close()
	if cap.usageInclude != true {
		t.Fatalf("upstream usage.include = %v, want true", cap.usageInclude)
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

func TestServerLogsActualServedFallbackModel(t *testing.T) {
	var cap capture
	// primary is test/cheap (p=0), but upstream's native fallback served test/mid.
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"x","model":"test/mid"}`)
	})
	defer up.Close()
	var log bytes.Buffer
	px := newProxy(t, mappedSnapshot(), up.URL, func(cfg *proxy.Config) { cfg.Logger = &log })
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.0","messages":[]}`, nil)
	resp.Body.Close()

	var entry struct {
		Model        string `json:"model"`
		FallbackHops int    `json:"fallback_hops"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(log.Bytes()), &entry); err != nil {
		t.Fatalf("decode log entry: %v\n%s", err, log.String())
	}
	if entry.Model != "test/mid" {
		t.Errorf("logged model = %q, want test/mid (the actually-served fallback)", entry.Model)
	}
	if entry.FallbackHops != 1 {
		t.Errorf("logged fallback_hops = %d, want 1", entry.FallbackHops)
	}
}

func TestServerLogsZeroHopsWhenPrimaryServed(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"x","model":"test/cheap"}`)
	})
	defer up.Close()
	var log bytes.Buffer
	px := newProxy(t, mappedSnapshot(), up.URL, func(cfg *proxy.Config) { cfg.Logger = &log })
	defer px.Close()

	resp := post(t, px.URL, `{"model":"pareto@0.0","messages":[]}`, nil)
	resp.Body.Close()

	var entry struct {
		Model        string `json:"model"`
		FallbackHops int    `json:"fallback_hops"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(log.Bytes()), &entry); err != nil {
		t.Fatalf("decode log entry: %v\n%s", err, log.String())
	}
	if entry.Model != "test/cheap" {
		t.Errorf("logged model = %q, want test/cheap", entry.Model)
	}
	if entry.FallbackHops != 0 {
		t.Errorf("logged fallback_hops = %d, want 0 when primary served", entry.FallbackHops)
	}
}

func TestServerLogsPassthroughModel(t *testing.T) {
	var cap capture
	up := fakeUpstream(t, &cap, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	defer up.Close()
	var log bytes.Buffer
	px := newProxy(t, mappedSnapshot(), up.URL, func(cfg *proxy.Config) { cfg.Logger = &log })
	defer px.Close()

	resp := post(t, px.URL, `{"model":"openai/gpt-4o","messages":[]}`, nil)
	resp.Body.Close()
	if !strings.Contains(log.String(), `"model":"openai/gpt-4o"`) {
		t.Fatalf("log missing passthrough model: %s", log.String())
	}
	if strings.Contains(log.String(), `"attempts"`) {
		t.Fatalf("log has synthetic attempts for passthrough request: %s", log.String())
	}
}

func TestServeModels(t *testing.T) {
	srv, err := proxy.NewServer(proxy.Config{
		Snapshot: &snapshot.Snapshot{Candidates: []snapshot.Candidate{
			{OpenRouterID: "test/model", Quality: 50, BlendedPricePer1M: 10, Slug: "test-model"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/models", nil)
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var res struct {
		Object string `json:"object"`
		Data   []struct {
			ID     string `json:"id"`
			Object string `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}

	if res.Object != "list" {
		t.Errorf("object = %q, want list", res.Object)
	}
	if len(res.Data) != 3 {
		t.Fatalf("len(data) = %d, want 3", len(res.Data))
	}

	wantIDs := []string{"pareto@0", "pareto@0.5", "pareto@1"}
	for i, want := range wantIDs {
		if res.Data[i].ID != want {
			t.Errorf("data[%d].id = %q, want %q", i, res.Data[i].ID, want)
		}
		if res.Data[i].Object != "model" {
			t.Errorf("data[%d].object = %q, want model", i, res.Data[i].Object)
		}
	}
}
