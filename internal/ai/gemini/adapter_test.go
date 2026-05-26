package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imattos78/agterm/internal/ai"
)

// geminiSSEServer builds a Gemini-style SSE response.
func geminiSSEServer(t *testing.T, tokens []string, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			fmt.Fprintf(w, `{"error":{"message":"test error","code":%d}}`, statusCode)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, tok := range tokens {
			event := map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"parts": []map[string]string{{"text": tok}},
						},
					},
				},
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
}

// newTestAdapter builds an Adapter wired to the given test server.
func newTestAdapter(t *testing.T, srv *httptest.Server) *Adapter {
	t.Helper()
	a := &Adapter{
		apiKey: "test-key",
		model:  "gemini-test",
		client: &http.Client{Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL}},
	}
	return a
}

func TestGemini_Stream_CollectsTokens(t *testing.T) {
	tokens := []string{"Hello", ", ", "world", "!"}
	srv := geminiSSEServer(t, tokens, http.StatusOK)
	defer srv.Close()

	a := newTestAdapter(t, srv)
	ch := a.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	})

	var got []string
	for r := range ch {
		if r.Done {
			if r.Err != nil {
				t.Fatalf("unexpected error: %v", r.Err)
			}
			break
		}
		got = append(got, r.Text)
	}
	if strings.Join(got, "") != "Hello, world!" {
		t.Errorf("got %q", strings.Join(got, ""))
	}
}

func TestGemini_Stream_HTTPError(t *testing.T) {
	srv := geminiSSEServer(t, nil, http.StatusForbidden)
	defer srv.Close()

	a := newTestAdapter(t, srv)
	ch := a.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	})

	var last ai.StreamResult
	for r := range ch {
		last = r
	}
	if !last.Done || last.Err == nil {
		t.Error("expected Done=true with non-nil error for HTTP 403")
	}
}

func TestGemini_Stream_ContextCancellation(t *testing.T) {
	srv := geminiSSEServer(t, []string{"tok1", "tok2"}, http.StatusOK)
	defer srv.Close()

	a := newTestAdapter(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := a.Stream(ctx, ai.Request{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	})
	var last ai.StreamResult
	for r := range ch {
		last = r
	}
	if !last.Done {
		t.Error("expected Done=true after context cancellation")
	}
}

// rewriteTransport replaces the Gemini API host with the test server.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.URL.Scheme = "http"
	r2.URL.Host = strings.TrimPrefix(rt.target, "http://")
	return rt.base.RoundTrip(r2)
}
