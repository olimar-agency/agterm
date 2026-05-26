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

| Layer    | Library                                |
|----------|----------------------------------------|
| TUI      | `charmbracelet/bubbletea` + `lipgloss` |
| PTY      | `creack/pty`                           |
| Raw mode | `golang.org/x/term`                    |
| Config   | stdlib `encoding/json`                 |
| HTTP     | stdlib `net/http`                      |

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
  "version": 1,
  "provider": "ollama",
  "providers": {
    "anthropic": {
      "api_key": "$ANTHROPIC_API_KEY",
      "model": "claude-sonnet-4-6",
      "send_context": false
    },
    "ollama": {
      "base_url": "http://localhost:11434",
      "model": "llama3.2"
    },
    "openrouter": {
      "api_key": "$OPENROUTER_API_KEY",
      "base_url": "https://openrouter.ai/api/v1",
      "model": "meta-llama/llama-3.1-8b-instruct:free",
      "send_context": false
    },
    "gemini": {
      "api_key": "$GEMINI_API_KEY",
      "model": "gemini-2.0-flash",
      "send_context": false
    }
  }
}
```

### Config lifecycle

- **Precedence** (highest → lowest): CLI flags → environment variables → config file → defaults
- **Env var expansion**: all `api_key` values support `$VAR` syntax, expanded at load time
- **Validation**: on startup, agterm validates config structure and logs warnings for missing/invalid AI settings — **the shell always starts regardless**; AI features show an inline error state rather than blocking the terminal
- **Versioning**: `"version"` field is written by agterm on first config save; if absent, loader assumes `version=0` and computes required migrations in memory. Migration detection (reading version, computing diff) happens on every load. The config file is only rewritten on an explicit write operation — `agterm migrate` command or saving a config change. A read-only startup (just launching the shell) never modifies the config file.
- **Schema version**: current is `1`; `--dry-run` on `agterm install` / `agterm migrate` shows pending changes without modifying files

---

## Development Phases

### Phase 1 — PTY Shell Passthrough ✅

**Goal**: working shell launched via agterm, transparent passthrough.

**Done means**: `go run ./cmd/agterm` launches the user's `$SHELL`, all input/output is forwarded correctly, terminal resize works, exit via `Ctrl+D` / `exit`.

- [x] Repo scaffolding, go.mod
- [x] `cmd/agterm/main.go`: spawn shell in PTY, raw mode stdin, SIGWINCH resize
- [x] `internal/pty/shell.go`: Shell struct (used Phase 2+)
- [x] `internal/block/block.go`: Block struct + Store (used Phase 2+)
- [x] `internal/ai/provider.go`: Provider interface (used Phase 3+)
- [ ] Shell integration scripts: `agterm install` injects OSC 133 hooks into shell RC file

  Shell integration details:
  - Idempotent injection (check for `# agterm-start` / `# agterm-end` sentinel comments before appending)
  - Backs up RC file before modifying (`~/.zshrc.agterm.bak`)
  - `agterm install --dry-run` prints what would be written without touching files
  - `agterm uninstall` cleanly removes lines between sentinels
  - **Hook strategy — always use `add-zsh-hook` for zsh** (safe with oh-my-zsh, prezto, any plugin manager):
    ```bash
    # agterm-start
    autoload -Uz add-zsh-hook
    _agterm_preexec() { printf '\x1b]133;C\x07'; }
    _agterm_precmd()  { printf '\x1b]133;D;%s\x07' "$?"; }
    add-zsh-hook preexec _agterm_preexec
    add-zsh-hook precmd  _agterm_precmd
    # agterm-end
    ```
  - Compatibility matrix:
    - `zsh`: `add-zsh-hook` (above) — works with oh-my-zsh, prezto, starship; truly additive
    - `bash`: append to `PROMPT_COMMAND`; use `trap DEBUG` for preexec equivalent
    - `fish`: `--on-event fish_preexec` / `fish_postexec` — additive by design, no special handling needed
  - Known conflicts: starship resets `precmd` hooks on some versions — detect starship in `$PATH` and emit a warning with link to workaround docs

---

### Phase 2 — Block Model + Bubbletea TUI

**Goal**: replace raw passthrough with block-structured UI.

**Done means**: every command and its output appears as a discrete styled block; exit code and duration are visible; scrolling through block history works; no raw escape sequences leak into the rendered output.

- [ ] `internal/pty/detector.go`: parse OSC 133 sequences, emit `BlockStart`/`BlockEnd` events
- [ ] `internal/block/parser.go`: accumulate PTY output between events into Blocks
- [ ] `internal/tui/model.go`: root Bubbletea model wiring blocks + input
- [ ] `internal/tui/blocks.go`: scrollable block list, Lipgloss styled
- [ ] `internal/tui/input.go`: input bar with command history (up/down)
- [ ] `internal/tui/styles.go`: theme (exit code colors, timestamps, borders)

**Failure modes**:
- OSC 133 hooks absent (user skipped `agterm install`): fall back to raw passthrough mode with a one-time prompt to run `agterm install`
- OSC marker missing mid-session (e.g. shell plugin conflict): fall back to regex-based prompt heuristic — scan output for common prompt patterns (`\$\s`, `%\s`, `❯\s`, `#\s` at end of line after a newline); this is best-effort and may split incorrectly; log mismatch to debug trace
- Partial output on session close: flush any open block as-is with `ExitCode = -1`

**Performance targets**:
- Keystroke → echo latency: < 10 ms (PTY round-trip, no rendering overhead)
- Startup time: < 200 ms to first prompt
- Block history in memory: cap at 500 blocks (~50 MB worst case); evict oldest

Block appearance:
```
❯ git log --oneline -5                              [0] 1.2s
  abc1234 add provider interface
  def5678 scaffold phase 1
```

---

### Phase 3 — First AI Provider

**Goal**: `Ctrl+A` opens AI panel, context-aware responses, auto-trigger on errors.

**Done means**: user can ask a question, get a streaming response using the configured provider, and the response cites the relevant command block. Auto-trigger fires on non-zero exit and can be dismissed.

- [ ] `internal/ai/context.go`: format last N blocks into AI prompt
- [ ] `internal/ai/anthropic/`: Anthropic streaming adapter
- [ ] `internal/tui/ai_panel.go`: streaming response panel (side or bottom)
- [ ] Auto-trigger on non-zero exit: "Command failed — explain?" prompt
- [ ] Keybind: `Ctrl+A` = open AI panel, `Esc` = close

**Failure modes**:
- Provider API timeout (> 15 s): show "Request timed out — retry?" inline
- Partial stream interrupted: display what arrived, mark response as incomplete
- No provider configured: show setup prompt pointing to config file

**Privacy / security**:
- **Default**: `send_context` is `false` for all remote providers — AI panel opens but only the user's typed question is sent, not command output, until the user explicitly opts in
- **Opt-in flow**: first time a user enables context for a remote provider, agterm shows a one-time consent notice listing exactly what will be sent; consent is recorded as `"send_context": true` in config
- **Precedence**: `local_only: true` in config overrides all per-provider `send_context` flags and prevents any outbound AI request; takes priority over everything except explicit CLI `--provider` flag which must also be a local provider or is rejected
- **Provider switch**: switching to a remote provider when `send_context` is still `false` silently sends only the user's question; a status bar indicator shows `[no context]` so the user knows
- Ollama is local-only; `send_context` field is ignored and no consent notice is shown
- Output is truncated to 4 000 chars per block before sending; full output never leaves the machine
- Secrets redaction: strip common patterns (API keys, tokens, passwords) from output before sending — configurable regex list in config

---

### Phase 4 — Multi-Provider

**Goal**: user can switch providers via config or `:provider <name>` command.

**Done means**: all four provider adapters work, hot-switch changes the active provider without restart, config env vars expand correctly.

- [ ] `internal/ai/ollama/`: Ollama adapter (local, free)
- [ ] `internal/ai/openai/`: OpenAI-compatible adapter — covers OpenRouter, Groq, Together, Mistral
- [ ] `internal/ai/gemini/`: Google Gemini adapter
- [ ] Provider registry + hot-switch (`:provider ollama`)
- [ ] Config loader: env var expansion, validation, version migration

**Failure modes**:
- Unknown provider name in config: print actionable error listing valid names
- API key missing for remote provider: prompt user to set env var or update config
- Ollama not running: surface "Ollama is not reachable at `<url>`" with start instructions

---

### Phase 5 — Agentic Features

**Goal**: AI can propose and execute multi-step tasks with explicit user confirmation at each step.

**Done means**: user describes a task in natural language, AI proposes a command sequence, user confirms step-by-step, commands execute and results feed back into AI context.

- [ ] AI suggests a command → shown in input bar highlighted → user confirms with Enter or rejects with Esc
- [ ] Multi-step tasks: "set up a Go project here" → AI runs a sequence
- [ ] Persistent block history across sessions — **decision: JSONL** (simpler than SQLite for append-only log; switch to SQLite if search/query features are needed)
  - Retention policy: keep last 30 days or 10 000 blocks, whichever comes first
  - Schema: one JSON object per line `{ "v":1, "block": {...} }`
- [ ] Safe read-only tool use: AI autonomously runs `ls`, `cat`, `git log` (whitelist enforced, not regex)
- [ ] Integration hook: dispatch long-running tasks to the `control` cloud plane via HTTP

---

## Key Technical Decision: Prompt Detection

OSC 133 semantic shell integration (same standard as Warp, iTerm2, Amazon Q).
Uses `add-zsh-hook` for zsh — safe with all plugin managers, no `eval` of stored function bodies:

```bash
# agterm-start  (injected into ~/.zshrc by `agterm install`)
autoload -Uz add-zsh-hook
_agterm_preexec() { printf '\x1b]133;C\x07'; }
_agterm_precmd()  { printf '\x1b]133;D;%s\x07' "$?"; }
add-zsh-hook preexec _agterm_preexec
add-zsh-hook precmd  _agterm_precmd
# agterm-end
```

`detector.go` parses these from the raw PTY byte stream without buffering full lines, so block boundaries are detected with minimal latency. When OSC markers are absent, falls back to regex prompt heuristic (best-effort).

---

## Testing Strategy

| Area | Approach |
|---|---|
| OSC 133 parsing | Unit tests with synthetic byte sequences including split-buffer edge cases |
| Block assembly | Table-driven tests: input PTY stream → expected Block slice |
| Config load/expand | Unit tests for env expansion, migration, validation errors |
| Provider streaming | Interface-level mocks; integration tests against Ollama (CI optional) |
| Shell hooks | Script tests: inject into temp RC file, verify idempotency and uninstall |
| TUI rendering | Snapshot tests via `bubbletea/teatest` |

Run all tests: `go test ./...`
Run integration tests (requires Ollama): `go test ./... -tags integration`

### Phase completion gates

A phase is not done until its gate tests pass. Merging to the default branch (`master` today — update this note if the repo is renamed to `main`) is blocked otherwise.

| Phase | Required tests before merge |
|---|---|
| Phase 1 | PTY spawns shell, resize works, exit is clean |
| Phase 2 | OSC parser handles split-buffer; block assembly table tests pass; hook inject + uninstall idempotency tests pass |
| Phase 3 | Provider mock streams correctly; consent flag respected (no context sent when `send_context=false`); timeout returns error, not panic |
| Phase 4 | All four provider adapters pass mock stream tests; env var expansion unit tests pass |
| Phase 5 | Command suggestion confirm/reject round-trip; JSONL append + retention eviction; whitelist enforced (blacklisted command not executed) |

---

## Observability & Debug

- `AGTERM_LOG=debug agterm` enables structured log output to `~/.config/agterm/debug.log`
- Logs are redacted with the same secrets patterns used before AI context is sent
- `agterm diag` prints a reproducible diagnostics bundle (OS, shell, Go version, config minus secrets, last 20 log lines) for bug reports
- Log levels: `debug`, `info`, `warn`, `error`

---

## Performance Targets

| Metric | Target |
|---|---|
| Keystroke → echo latency | < 10 ms |
| Startup to first prompt | < 200 ms |
| Block history (memory) | ≤ 500 blocks in memory |
| AI first token (remote) | < 2 s (network-dependent, surfaced as a loading indicator) |
| Binary size | < 20 MB |

---

## Packaging & Distribution

- [ ] Cross-compile targets: `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`
- [ ] GitHub Actions release workflow: tag → build matrix → upload binaries + checksums
- [ ] **Integrity**: each release publishes a `checksums.sha256` file signed with a project key (cosign or GPG); install script verifies checksum before executing binary
- [ ] Install script (convenience): `curl -fsSL https://agterm.sh/install | sh` — this is a floating URL for discoverability only; security-conscious users should use the checksum-verified flow:
  ```sh
  VERSION=v0.1.0  # pin to a release tag
  curl -fsSL "https://github.com/olimar-agency/agterm/releases/download/${VERSION}/install.sh" -o install.sh
  curl -fsSL "https://github.com/olimar-agency/agterm/releases/download/${VERSION}/checksums.sha256" | grep install.sh | sha256sum --check
  sh install.sh
  ```
- [ ] Homebrew tap: `brew install olimar-agency/tap/agterm`
- [ ] Versioning: semver, embedded in binary via `-ldflags "-X main.version=..."`
- [ ] Release notes generated from conventional commits

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
| Data privacy | Output sent to Warp servers | Local-only mode available |
