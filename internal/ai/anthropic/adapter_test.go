package anthropic

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

// sseServer builds a minimal SSE response with the given text tokens.
func sseServer(t *testing.T, tokens []string, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			fmt.Fprintf(w, `{"error":{"message":"test error"}}`)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		for _, tok := range tokens {
			event := map[string]any{
				"type": "content_block_delta",
				"delta": map[string]string{
					"type": "text_delta",
					"text": tok,
				},
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		// send done sentinel
		fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
}

func TestAdapter_Stream_CollectsTokens(t *testing.T) {
	tokens := []string{"Hello", ", ", "world", "!"}
	srv := sseServer(t, tokens, http.StatusOK)
	defer srv.Close()

	a := &Adapter{
		apiKey: "test",
		model:  "claude-test",
		client: &http.Client{
			Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
		},
	}

	ch := a.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	})

	var collected []string
	for r := range ch {
		if r.Done {
			if r.Err != nil {
				t.Fatalf("unexpected error: %v", r.Err)
			}
			break
		}
		collected = append(collected, r.Text)
	}

	got := strings.Join(collected, "")
	want := "Hello, world!"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestAdapter_Stream_HTTPError(t *testing.T) {
	srv := sseServer(t, nil, http.StatusUnauthorized)
	defer srv.Close()

	a := &Adapter{
		apiKey: "bad-key",
		model:  "claude-test",
		client: &http.Client{
			Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
		},
	}

	ch := a.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	})

	var lastResult ai.StreamResult
	for r := range ch {
		lastResult = r
	}
	if !lastResult.Done || lastResult.Err == nil {
		t.Error("expected Done=true with non-nil error for HTTP 401")
	}
}

func TestAdapter_Stream_ContextCancellation(t *testing.T) {
	srv := sseServer(t, []string{"tok1", "tok2"}, http.StatusOK)
	defer srv.Close()

	a := &Adapter{
		apiKey: "test",
		model:  "claude-test",
		client: &http.Client{
			Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	ch := a.Stream(ctx, ai.Request{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	})

	var last ai.StreamResult
	for r := range ch {
		last = r
	}
	// context cancelled — should be Done with nil error (not surfaced as error)
	if !last.Done {
		t.Error("expected Done=true after context cancellation")
	}
}

// rewriteTransport replaces the host of every request with target.
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
