package ai

import "context"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type Request struct {
	System   string
	Messages []Message
}

// StreamResult is one item from a streaming AI response.
// Text carries the token text. Done is true on the final item (whether or not
// Err is set). The provider closes the channel after sending Done=true.
type StreamResult struct {
	Text string
	Err  error
	Done bool
}

// Provider is the interface all AI backends must implement.
// Stream starts an async response and returns a channel the caller ranges over.
// The provider is responsible for closing the channel; Done=true on the last item.
type Provider interface {
	Name() string
	Stream(ctx context.Context, req Request) <-chan StreamResult
}
