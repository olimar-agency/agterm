package gemini

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
	ai.Register("gemini", func(apiKey, baseURL, model string) ai.Provider {
		return New(config.ProviderConfig{APIKey: apiKey, Model: model})
	})
}

const (
	apiBase      = "https://generativelanguage.googleapis.com/v1beta/models"
	defaultModel = "gemini-2.0-flash"
)

// Adapter implements ai.Provider for Google Gemini via the REST API.
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
	return &Adapter{apiKey: cfg.APIKey, model: model, client: &http.Client{}}
}

func (a *Adapter) Name() string { return "gemini" }

// Stream starts a streaming generateContent request and returns a channel of StreamResults.
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
	body := map[string]any{
		"contents": toContents(req.Messages),
	}
	if req.System != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]string{{"text": req.System}},
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/%s:streamGenerateContent?key=%s&alt=sse", apiBase, a.model, a.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
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
				Code    int    `json:"code"`
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
		if data == "" {
			continue
		}

		var event struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if len(event.Candidates) > 0 && len(event.Candidates[0].Content.Parts) > 0 {
			text := event.Candidates[0].Content.Parts[0].Text
			if text != "" {
				select {
				case ch <- ai.StreamResult{Text: text}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}

	return scanner.Err()
}

// toContents converts ai.Messages into Gemini's contents format.
// Gemini uses "user" and "model" roles (not "assistant").
func toContents(msgs []ai.Message) []map[string]any {
	out := make([]map[string]any, len(msgs))
	for i, m := range msgs {
		role := string(m.Role)
		if role == "assistant" {
			role = "model"
		}
		out[i] = map[string]any{
			"role":  role,
			"parts": []map[string]string{{"text": m.Content}},
		}
	}
	return out
}
