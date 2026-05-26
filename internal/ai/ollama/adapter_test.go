package ollama

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

// ndjsonServer serves streaming NDJSON chunks the way Ollama does.
func ndjsonServer(t *testing.T, tokens []string, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			fmt.Fprintf(w, `{"error":"test error"}`)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		for i, tok := range tokens {
			done := i == len(tokens)-1
			chunk := map[string]any{
				"message": map[string]string{"role": "assistant", "content": tok},
				"done":    done,
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "%s\n", data)
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
}

func newAdapter(t *testing.T, srv *httptest.Server) *Adapter {
	t.Helper()
	return &Adapter{
		baseURL: srv.URL,
		model:   "llama-test",
		client:  &http.Client{},
	}
}

func TestOllama_Stream_CollectsTokens(t *testing.T) {
	tokens := []string{"Hello", ", ", "world", "!"}
	srv := ndjsonServer(t, tokens, http.StatusOK)
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

func TestOllama_Stream_HTTPError(t *testing.T) {
	srv := ndjsonServer(t, nil, http.StatusInternalServerError)
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
		t.Error("expected Done=true with non-nil error for HTTP 500")
	}
}

func TestOllama_Stream_ContextCancellation(t *testing.T) {
	srv := ndjsonServer(t, []string{"tok1", "tok2"}, http.StatusOK)
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
