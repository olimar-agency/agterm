# agterm — Agentic Terminal

An open-source, agentic terminal built in Go. Inspired by Warp but model-agnostic: works with any AI provider including free and local ones.

---

## Goals

- Block-based command output (each command + output is a structured unit)
- AI assistance inline, triggered by the user or auto-triggered on errors
- Any AI provider: Anthropic, OpenAI, Ollama (local/free), Gemini, OpenRouter (free tier)
- Single Go binary — no Electron, no cloud account required
- Works over SSH

---

## Tech Stack

| Layer      | Library                                    |
|------------|--------------------------------------------|
| TUI        | `charmbracelet/bubbletea` + `lipgloss`     |
| PTY        | `creack/pty`                               |
| Raw mode   | `golang.org/x/term`                        |
| Config     | stdlib `encoding/json`                     |
| HTTP       | stdlib `net/http`                          |

---

## Architecture

```
┌─────────────────────────────────────────┐
│           TUI Layer (Bubbletea)          │
│   input bar │ block list │ AI panel      │
└──────────────────┬──────────────────────┘
                   │
┌──────────────────▼──────────────────────┐
│             Block Manager                │
│  Block { command, output, exit, dur }    │
└──────────┬───────────────────────────────┘
           │                    │
┌──────────▼──────┐   ┌────────▼────────────┐
│   Shell / PTY   │   │   AI Provider Layer  │
│   creack/pty    │   │   Provider interface │
│   prompt detect │   │   Anthropic / OpenAI │
│   shell scripts │   │   Ollama / Gemini    │
└─────────────────┘   └─────────────────────┘
```

---

## File Structure

```
agterm/
├── cmd/agterm/main.go          # entry point
├── internal/
│   ├── pty/
│   │   ├── shell.go            # Shell struct, PTY management
│   │   └── detector.go         # prompt boundary detection (Phase 2)
│   ├── block/
│   │   ├── block.go            # Block struct + Store
│   │   └── parser.go           # output → blocks (Phase 2)
│   ├── tui/
│   │   ├── model.go            # root Bubbletea model (Phase 2)
│   │   ├── blocks.go           # block list renderer
│   │   ├── input.go            # input bar + history
│   │   ├── ai_panel.go         # streaming AI response panel
│   │   └── styles.go           # Lipgloss theme
│   ├── ai/
│   │   ├── provider.go         # Provider interface + types
│   │   ├── context.go          # builds prompt from block history
│   │   ├── anthropic/          # Anthropic adapter
│   │   ├── openai/             # OpenAI-compatible (Groq, OpenRouter, Together)
│   │   ├── ollama/             # Ollama local adapter
│   │   └── gemini/             # Google Gemini adapter
│   └── config/
│       └── config.go           # Config struct, load from ~/.config/agterm/config.json
├── plan.md
├── go.mod
└── .gitignore
```

---

## Core Interfaces

```go
// AI provider abstraction
type Provider interface {
    Name() string
    Stream(ctx context.Context, req Request, out chan<- string) error
}

// Block model
type Block struct {
    ID        string
    Command   string
    Output    string
    ExitCode  int
    Duration  time.Duration
    WorkDir   string
    StartedAt time.Time
}
```

---

## Config (`~/.config/agterm/config.json`)

```json
{
  "provider": "ollama",
  "providers": {
    "anthropic": {
      "api_key": "$ANTHROPIC_API_KEY",
      "model": "claude-sonnet-4-6"
    },
    "ollama": {
      "base_url": "http://localhost:11434",
      "model": "llama3.2"
    },
    "openrouter": {
      "api_key": "$OPENROUTER_API_KEY",
      "base_url": "https://openrouter.ai/api/v1",
      "model": "meta-llama/llama-3.1-8b-instruct:free"
    },
    "gemini": {
      "api_key": "$GEMINI_API_KEY",
      "model": "gemini-2.0-flash"
    }
  }
}
```

---

## Development Phases

### Phase 1 — PTY Shell Passthrough (current)

**Goal**: working shell launched via agterm, transparent passthrough.

- [x] Repo scaffolding, go.mod
- [x] `cmd/agterm/main.go`: spawn shell in PTY, raw mode stdin, SIGWINCH resize
- [x] `internal/pty/shell.go`: Shell struct (used Phase 2+)
- [x] `internal/block/block.go`: Block struct + Store (used Phase 2+)
- [x] `internal/ai/provider.go`: Provider interface (used Phase 3+)
- [ ] Shell integration scripts: inject hooks into `.bashrc`/`.zshrc` that emit
      boundary markers before/after each command so we know exactly where one
      command ends and the next begins

### Phase 2 — Block Model + Bubbletea TUI

**Goal**: replace raw passthrough with block-structured UI.

- [ ] `internal/pty/detector.go`: parse escape sequences from shell hooks, emit `BlockStart`/`BlockEnd` events
- [ ] `internal/block/parser.go`: accumulate output between events into Blocks
- [ ] `internal/tui/model.go`: root Bubbletea model wiring blocks + input
- [ ] `internal/tui/blocks.go`: scrollable block list, Lipgloss styled
- [ ] `internal/tui/input.go`: input bar with command history (up/down)
- [ ] `internal/tui/styles.go`: theme (exit code colors, timestamps, borders)

Block appearance:
```
❯ git log --oneline -5                              [0] 1.2s
  abc1234 add provider interface
  def5678 scaffold phase 1
```

### Phase 3 — First AI Provider

**Goal**: Ctrl+A opens AI panel, context-aware responses.

- [ ] `internal/ai/context.go`: format last N blocks into AI prompt
- [ ] `internal/ai/anthropic/`: Anthropic streaming adapter
- [ ] `internal/tui/ai_panel.go`: streaming response panel (side or bottom)
- [ ] Auto-trigger on non-zero exit: "Command failed — explain?" prompt
- [ ] Keybind: `Ctrl+A` = open AI panel, `Esc` = close

### Phase 4 — Multi-Provider

**Goal**: user can switch providers via config or `:provider <name>` command.

- [ ] `internal/ai/ollama/`: Ollama adapter (local, free)
- [ ] `internal/ai/openai/`: OpenAI-compatible adapter — covers OpenRouter, Groq, Together, Mistral with one implementation
- [ ] `internal/ai/gemini/`: Google Gemini adapter
- [ ] Provider registry + hot-switch
- [ ] Config loader with env var expansion (`$ANTHROPIC_API_KEY`)

### Phase 5 — Agentic Features

**Goal**: AI can propose and execute multi-step tasks.

- [ ] AI suggests a command → shown in input bar → user confirms with Enter
- [ ] Multi-step tasks: "set up a Go project here" → AI runs a sequence
- [ ] Persistent block history across sessions (SQLite or JSONL)
- [ ] Safe read-only tool use: AI can autonomously run `ls`, `cat`, `git log`
- [ ] Integration hook: dispatch long-running tasks to the `control` cloud plane

---

## Key Technical Decision: Prompt Detection

The shell needs to emit markers so agterm knows when a command starts and ends.
Shell integration scripts (same approach as Warp, iTerm2, Amazon Q):

```bash
# injected into ~/.zshrc by `agterm install`
preexec() { printf '\x1b]133;C\x07'; }
precmd()  { printf '\x1b]133;D;%s\x07' "$?"; }
```

These OSC 133 sequences are the de-facto standard for semantic shell integration.
`detector.go` parses them from the PTY output stream to emit block boundaries.

---

## What Makes This Different from Warp

| Feature | Warp | agterm |
|---|---|---|
| License | AGPL-3.0, closed AI backend | Open source |
| AI provider | Warp AI only | Any provider |
| Free tier | Limited | Ollama (fully local), OpenRouter free models |
| Distribution | macOS/Linux app | Single Go binary, works over SSH |
| Account required | Yes | No |
| Build time | ~5 min (Rust, 60+ crates) | Seconds (Go) |
