package anthropic

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

const (
	apiURL       = "https://api.anthropic.com/v1/messages"
	apiVersion   = "2023-06-01"
	defaultModel = "claude-sonnet-4-6"
)

// Adapter implements ai.Provider using the Anthropic Messages API with
// server-sent events (SSE) streaming over stdlib net/http.
type Adapter struct {
	apiKey string
	model  string
	client *http.Client
}

// New creates an Adapter from the given provider config.
func New(cfg config.ProviderConfig) *Adapter {
	model := cfg.Model
	if model == "" {
		model = defaultModel
	}
	return &Adapter{
		apiKey: cfg.APIKey,
		model:  model,
		client: &http.Client{},
	}
}

func (a *Adapter) Name() string { return "anthropic" }

// Stream starts a streaming Messages request and returns a channel of
// StreamResults. The channel is closed after sending Done=true.
func (a *Adapter) Stream(ctx context.Context, req ai.Request) <-chan ai.StreamResult {
	ch := make(chan ai.StreamResult, 64)
	go func() {
		defer close(ch)
		err := a.stream(ctx, req, ch)
		if ctx.Err() != nil {
			err = nil // context cancellation is not an error
		}
		ch <- ai.StreamResult{Done: true, Err: err}
	}()
	return ch
}

// stream performs the HTTP request and parses SSE events, sending text deltas
// to ch. Returns a non-nil error only for genuine failures.
func (a *Adapter) stream(ctx context.Context, req ai.Request, ch chan<- ai.StreamResult) error {
	body := map[string]any{
		"model":      a.model,
		"max_tokens": 2048,
		"stream":     true,
		"messages":   toMessages(req.Messages),
	}
	if req.System != "" {
		body["system"] = req.System
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, errBody.Error.Message)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
			select {
			case ch <- ai.StreamResult{Text: event.Delta.Text}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return scanner.Err()
}

func toMessages(msgs []ai.Message) []map[string]string {
	out := make([]map[string]string, len(msgs))
	for i, m := range msgs {
		out[i] = map[string]string{"role": string(m.Role), "content": m.Content}
	}
	return out
}
