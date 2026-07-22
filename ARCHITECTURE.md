# agterm — Architecture & Phase 6 Contract

> Fuente: contrato validado por el **Architect Agent** en el issue #2 (`/architect-agent design`).
> Este documento es el input contra el que se implementa **Phase 6 — ANSI Rendering** y las fases posteriores del roadmap Terminal Emulator (Phases 6–12). Los boundaries marcados como **no negociables** no deben reabrirse sin volver a pasar por Architect.

---

## 1. Estado actual (packages reales)

```
internal/
  pty/        shell.go, detector.go     # PTY + OSC 133 prompt detection (código más sólido)
  block/      block.go, parser.go       # Block struct + Store; parser de segmentos
  tui/        model.go, styles.go       # Bubbletea (model.go ~690 LOC, monolítico)
  ai/         provider.go, registry.go, suggest.go, context.go, tools/whitelist.go
              anthropic/ openai/ ollama/ gemini/   # adapters multi-provider
  history/    recorder.go               # JSONL history (~/.local/share/agterm/history.jsonl)
  hook/       dispatcher.go             # :dispatch → control plane (DEAD CODE, ver §7)
  config/     config.go
```

`Block` (en `internal/block/block.go`) hoy es texto plano:

```go
type Block struct {
    ID        string
    Command   string
    Output    string   // texto plano — source of truth actual
    ExitCode  int
    Duration  time.Duration
    WorkDir   string
    StartedAt time.Time
}
```

---

## 2. Contrato `Cell` (estable Phase 6 → Phase 12)

Nuevo package `internal/vt/`. Definir `Cell` **una sola vez**; debe ser lo bastante rico para ANSI real sin acoplarse a decisiones VT profundas (Phase 8).

```go
// internal/vt/cell.go
package vt

type ColorMode uint8

const (
    ColorDefault ColorMode = iota
    ColorIndexed16
    ColorIndexed256
    ColorRGB
)

type Color struct {
    Mode  ColorMode
    Value uint32 // índice (16/256) o RGB empaquetado (0xRRGGBB)
}

// Attr es un bitmask; evita inflar el struct.
type Attr uint16

const (
    AttrBold Attr = 1 << iota
    AttrDim
    AttrItalic
    AttrUnderline
    AttrBlink
    AttrReverse
    AttrStrike
    AttrHidden
)

type Cell struct {
    Rune rune
    FG   Color
    BG   Color
    Attr Attr
    // Width byte  // reservado para wide chars (CJK) — Phase 8, NO implementar ahora
}

// Line permite cambiar el backing store en Phase 7 (scrollback) sin tocar consumidores.
type Line []Cell
```

**Racional:**
- `Color` es struct propio (no `lipgloss.Color`) → `internal/vt/` queda **sin dependencias de rendering**. `lipgloss` vive solo en `internal/tui/`, que traduce `Cell → lipgloss.Style` al pintar.
- `Attr` como bitmask: una pantalla de 200×50 = 10K cells; el tamaño del struct importa.
- `Width` reservado como comentario (no se implementa) para evitar un ABI break en Phase 8.
- `type Line []Cell` es **la unidad que consumen `Block.Cells` y `PlainText`** (no `[][]Cell` crudo). Así el backing store de una línea puede cambiar a un pool/ring en Phase 7 sin tocar consumidores.

---

## 3. Transición `Block.Output → Block.Cells` (aditiva, no destructiva)

Phase 5 (agentic) ya consume `Block.Output string` para construir contexto de IA. Eliminar `Output` rompería el auto-trigger y el Provider. Por eso, durante Phases 6–9 **ambos campos coexisten**:

```go
type Block struct {
    ID        string
    Command   string
    Output    string   // DEPRECATED post-Phase 9; mantener durante 6–8
    Cells     []Line   // source of truth desde Phase 6 (Line = []Cell)
    ExitCode  int
    Duration  time.Duration
    WorkDir   string
    StartedAt time.Time
}
```

- **Phase 6:** escribir `Cells` + derivar `Output` plano (strip ANSI) para no romper la IA.
- **Phase 9:** la IA pasa a consumir `Cells` (o una vista textual/semántica derivada). En ese momento se elimina `Output`.
- Resultado: Phase 6 es un cambio **aditivo**; se puede mergear sin congelar la capa AI.

**Invariante de test (obligatorio hasta que `Output` desaparezca):**
> Para todo `Block`: `vt.PlainText(b.Cells) == b.Output`.

---

## 4. Boundaries de `internal/vt/` (NO NEGOCIABLES)

```
internal/vt/
  cell.go       # Cell, Color, Attr, Line — tipos puros, cero deps
  parser.go     # bytes del PTY → eventos (CSI, SGR, texto, control)
  buffer.go     # grid de Cells + cursor (lo mínimo para Phase 6)
  view.go       # PlainText(cells) — puente hacia la IA
  # scrollback.go → Phase 7
  # terminal.go  → Phase 8 (state machine completa)
  # inject.go    → Phase 9
```

Reglas que el implementador **no puede reabrir** sin volver a Architect:
1. `internal/vt/` **no importa** `bubbletea`, `lipgloss`, ni nada de `internal/tui/` ni `internal/ai/`.
2. `internal/tui/` es el **único** que conoce `lipgloss`; traduce `Cell → estilo visual`.
3. `internal/ai/` consume una **vista** del Block (texto plano derivado o representación semántica), **nunca** `Cells` directos → el `Provider` interface queda intacto.
4. El **parser emite eventos**, no muta buffers; el **buffer aplica eventos**. Esto permite testear el parser aislado, sin PTY real (mismo patrón que `pty/detector.go`).

---

## 5. Puente hacia la IA (preparar Phase 9 desde Phase 6)

```go
// internal/vt/view.go
func PlainText(cells []Line) string // strip de estilos; para la IA en fase transicional
```

En Phase 9 se añadirá `SemanticText` (marcadores tipo `error`, `path`, `url`), pero `PlainText` basta para no bloquear la Phase 5 mientras Phase 6 avanza. **El `Provider` interface no cambia.**

---

## 6. Refactor mínimo de `model.go` (dentro de Phase 6, no fase aparte)

No hacer el refactor completo del monolito ahora. Extraer **solo** lo necesario:
- El fragmento que hoy acumula `output string` desde el PTY → mover a un `PTYReader` que emite bytes hacia `vt.Parser`.
- Todo lo demás (input, rendering, panel IA) permanece en `model.go` hasta después de Phase 7.

Racional: un refactor grande + Phase 6 en el mismo slice multiplica el riesgo. Se acota deliberadamente.

---

## 7. `:dispatch` — dead code (retirar)

`internal/hook/dispatcher.go` POSTea `descripción + blocks` a un control plane que **no existe** todavía. Wiring en `internal/tui/model.go` (`dispatchDoneMsg`, `dispatcher`, `dispatchCmd`, handler de `:dispatch `). Decisión unánime PM + Architect: **retirar ahora** y trackear en issue de seguimiento. Motivo: da falsa sensación de feature; reintroducir cuando el control plane tenga contrato estable para agentes externos. Ver sub-issue de retiro.

---

## 8. Set mínimo obligatorio del parser (Phase 6)

Para no degradar el contexto que recibe la IA en Phase 9, el parser de Phase 6 **debe** cubrir:
- **SGR**: colores 16 / 256 / truecolor + atributos básicos (bold, dim, italic, underline, reverse) + reset.
- **CR / LF / BS / tabs**.

Todo lo demás (cursor positioning, char sets, DCS complejos) puede ser **consumido sin efecto** en Phase 6 y completarse en Phase 8.

---

## 9. Riesgos registrados

| # | Riesgo | Mitigación |
|---|--------|------------|
| 1 | `Cell` crece en Phase 8 (wide chars, OSC 8 hyperlinks, sixel) | `Width` reservado + `Attr` bitmask extensible |
| 2 | Doble source of truth `Output`+`Cells` (6–9) diverge | Test invariante `PlainText(Cells) == Output` |
| 3 | Parser incompleto degrada IA en Phase 9 | Set mínimo obligatorio (§8) |
| 4 | Performance de `[]Line` en scrollback (Phase 7) | Consumidores usan `[]Line` → cambiar el backing store de `Line` (pool/ring) sin tocarlos |
| 5 | Regresión sobre OSC 133 (`detector.go` comparte input stream) | Gate QA: 61 tests + suite ANSI antes de merge |
| 6 | `charmbracelet/x/ansi` es lib joven | Validar madurez / alternativas antes de comprometer |

---

## Orden de ejecución del roadmap

`6 (ANSI)` → `9 (IA styled)` → `7 (scrollback)` → `11 (config)` → `8 (VT full)` → `10 (tabs)` → `12 (mouse/paste)`.

Ver el roadmap completo y el estado verificado en el **issue #2**.
