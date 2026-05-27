package openai

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

// compatibleProviders lists OpenAI-compatible services that use this adapter.
// Each maps to its default base URL and default model.
var compatibleProviders = []struct {
	name     string
	baseURL  string
	defModel string
}{
	{"openai", "https://api.openai.com", "gpt-4o-mini"},
	{"deepseek", "https://api.deepseek.com", "deepseek-chat"},
	{"openrouter", "https://openrouter.ai/api", "deepseek/deepseek-chat-v3-0324:free"},
	{"groq", "https://api.groq.com/openai", "llama-3.3-70b-versatile"},
	{"together", "https://api.together.xyz", "meta-llama/Llama-3.3-70B-Instruct-Turbo"},
	{"mistral", "https://api.mistral.ai", "mistral-small-latest"},
}

func init() {
	for _, p := range compatibleProviders {
		p := p // capture
		ai.Register(p.name, func(apiKey, baseURL, model string) ai.Provider {
			if baseURL == "" {
				baseURL = p.baseURL
			}
			if model == "" {
				model = p.defModel
			}
			return New(config.ProviderConfig{APIKey: apiKey, BaseURL: baseURL, Model: model})
		})
	}
}

const (
	defaultBaseURL = "https://api.openai.com"
	defaultModel   = "gpt-4o-mini"
)

// Adapter implements ai.Provider for any OpenAI-compatible API endpoint.
// This covers OpenAI, OpenRouter, Groq, Together, Mistral, and others that
// expose a /v1/chat/completions endpoint with the same SSE format.
type Adapter struct {
	baseURL string
	apiKey  string
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
	return &Adapter{
		baseURL: base,
		apiKey:  cfg.APIKey,
		model:   model,
		client:  &http.Client{},
	}
}

func (a *Adapter) Name() string { return "openai" }

// Stream starts a streaming chat completions request and returns a channel of StreamResults.
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

	url := a.baseURL + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
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
		msg := errBody.Error.Message
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				break
			}
			continue
		}

		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if len(event.Choices) > 0 && event.Choices[0].Delta.Content != "" {
			select {
			case ch <- ai.StreamResult{Text: event.Choices[0].Delta.Content}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return scanner.Err()
}

// toMessages converts an ai.Request into OpenAI's message format, prepending
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
