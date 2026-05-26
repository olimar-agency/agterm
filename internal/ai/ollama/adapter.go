package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/imattos78/agterm/internal/ai"
	"github.com/imattos78/agterm/internal/config"
)

func init() {
	ai.Register("ollama", func(apiKey, baseURL, model string) ai.Provider {
		return New(config.ProviderConfig{BaseURL: baseURL, Model: model})
	})
}

const (
	defaultBaseURL = "http://localhost:11434"
	defaultModel   = "llama3.2"
)

// Adapter implements ai.Provider for a locally running Ollama instance.
// It uses the /api/chat endpoint with streaming NDJSON responses.
type Adapter struct {
	baseURL string
	model   string
	client  *http.Client
}

// New creates an Adapter from the given provider config.
func New(cfg config.ProviderConfig) *Adapter {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	model := cfg.Model
	if model == "" {
		model = defaultModel
	}
	return &Adapter{baseURL: base, model: model, client: &http.Client{}}
}

func (a *Adapter) Name() string { return "ollama" }

// Stream starts a streaming chat request and returns a channel of StreamResults.
func (a *Adapter) Stream(ctx context.Context, req ai.Request) <-chan ai.StreamResult {
	ch := make(chan ai.StreamResult, 64)
	go func() {
		defer close(ch)
		err := a.stream(ctx, req, ch)
		if ctx.Err() != nil {
			err = nil
		}
		ch <- ai.StreamResult{Done: true, Err: err}
	}()
	return ch
}

func (a *Adapter) stream(ctx context.Context, req ai.Request, ch chan<- ai.StreamResult) error {
	msgs := toMessages(req)

	body := map[string]any{
		"model":    a.model,
		"messages": msgs,
		"stream":   true,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := a.baseURL + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		// surface a friendly error when Ollama is not running
		if isConnectionRefused(err) {
			return fmt.Errorf("Ollama is not reachable at %s — run `ollama serve` to start it", a.baseURL)
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		msg := errBody.Error
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	// Ollama streams NDJSON: one JSON object per line
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}

		if chunk.Message.Content != "" {
			select {
			case ch <- ai.StreamResult{Text: chunk.Message.Content}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if chunk.Done {
			break
		}
	}

	return scanner.Err()
}

// toMessages converts an ai.Request into Ollama's message format, prepending
// the system prompt as a system-role message when present.
func toMessages(req ai.Request) []map[string]string {
	var msgs []map[string]string
	if req.System != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	return msgs
}

func isConnectionRefused(err error) bool {
	return err != nil && strings.Contains(err.Error(), "connection refused")
}
