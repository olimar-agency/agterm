package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/imattos78/agterm/internal/block"
)

// Payload is what gets POST-ed to the control plane.
type Payload struct {
	Description string         `json:"description"`
	Blocks      []*block.Block `json:"blocks"`
	SentAt      time.Time      `json:"sent_at"`
}

// Dispatcher sends task payloads to the control plane URL.
type Dispatcher struct {
	url    string
	token  string
	client *http.Client
}

// New creates a Dispatcher. url and token come from config.Control.
func New(url, token string) *Dispatcher {
	return &Dispatcher{
		url:    url,
		token:  token,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Dispatch POSTs description + recent blocks to the control plane.
// It is non-blocking: the caller should invoke it in a goroutine.
// Returns an error string for display, or "" on success.
func (d *Dispatcher) Dispatch(description string, blocks []*block.Block) error {
	if d.url == "" {
		return fmt.Errorf("control.url not set in config")
	}

	payload := Payload{
		Description: description,
		Blocks:      blocks,
		SentAt:      time.Now().UTC(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.token != "" {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("dispatch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("control plane returned HTTP %d", resp.StatusCode)
	}
	return nil
}
