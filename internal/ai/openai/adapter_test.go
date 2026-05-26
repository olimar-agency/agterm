package openai

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

// sseServer builds an OpenAI-style SSE response.
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
				"choices": []map[string]any{
					{"delta": map[string]string{"content": tok}},
				},
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
}

func newAdapter(t *testing.T, srv *httptest.Server) *Adapter {
	t.Helper()
	return &Adapter{
		baseURL: srv.URL,
		apiKey:  "test",
		model:   "gpt-test",
		client:  &http.Client{},
	}
}

func TestOpenAI_Stream_CollectsTokens(t *testing.T) {
	tokens := []string{"Hello", ", ", "world", "!"}
	srv := sseServer(t, tokens, http.StatusOK)
	defer srv.Close()

	a := newAdapter(t, srv)
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

func TestOpenAI_Stream_HTTPError(t *testing.T) {
	srv := sseServer(t, nil, http.StatusUnauthorized)
	defer srv.Close()

	a := newAdapter(t, srv)
	ch := a.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	})

	var last ai.StreamResult
	for r := range ch {
		last = r
	}
	if !last.Done || last.Err == nil {
		t.Error("expected Done=true with non-nil error for HTTP 401")
	}
}

func TestOpenAI_Stream_ContextCancellation(t *testing.T) {
	srv := sseServer(t, []string{"tok1", "tok2"}, http.StatusOK)
	defer srv.Close()

	a := newAdapter(t, srv)
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
