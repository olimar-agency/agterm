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

type Provider interface {
	Name() string
	Stream(ctx context.Context, req Request, out chan<- string) error
}
