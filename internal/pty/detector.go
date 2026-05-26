package pty

import (
	"bytes"
	"fmt"
)

// SegKind classifies a segment returned by the Detector.
type SegKind int

const (
	SegOutput       SegKind = iota // raw terminal output (OSC sequences stripped)
	SegCommandStart                // OSC 133;C — shell is about to execute a command
	SegCommandEnd                  // OSC 133;D — command finished
)

// Segment is one ordered unit of PTY output. Segments are returned in the
// order they appear in the byte stream, so output before a SegCommandEnd is
// correctly attributed to the closing block.
type Segment struct {
	Kind     SegKind
	Data     []byte // non-nil only for SegOutput
	ExitCode int    // populated for SegCommandEnd
}

var oscPrefix = []byte("\x1b]133;")

// Detector strips OSC 133 semantic-shell sequences from raw PTY output and
// returns an ordered slice of Segments. Safe for use from one goroutine only.
type Detector struct {
	buf []byte
}

// Process takes a raw PTY read buffer and returns ordered segments. The
// internal buffer retains any incomplete OSC sequence across calls.
func (d *Detector) Process(raw []byte) []Segment {
	d.buf = append(d.buf, raw...)
	var segs []Segment

	for {
		idx := bytes.Index(d.buf, oscPrefix)
		if idx < 0 {
			// guard: partial OSC prefix at end of buffer — keep it, emit the rest
			if cut := partialSuffix(d.buf, oscPrefix); cut >= 0 {
				if cut > 0 {
					segs = appendOutput(segs, d.buf[:cut])
				}
				d.buf = d.buf[cut:]
				return segs
			}
			if len(d.buf) > 0 {
				segs = appendOutput(segs, d.buf)
				d.buf = nil
			}
			return segs
		}

		// emit output that precedes this OSC sequence
		if idx > 0 {
			segs = appendOutput(segs, d.buf[:idx])
		}

		rest := d.buf[idx+len(oscPrefix):]

		// find BEL terminator (\x07); incomplete sequence stays buffered
		end := bytes.IndexByte(rest, '\x07')
		if end < 0 {
			d.buf = d.buf[idx:]
			return segs
		}

		seq := rest[:end]
		d.buf = rest[end+1:]

		switch {
		case bytes.Equal(seq, []byte("C")):
			segs = append(segs, Segment{Kind: SegCommandStart})
		case bytes.HasPrefix(seq, []byte("D;")):
			var code int
			fmt.Sscanf(string(seq[2:]), "%d", &code)
			segs = append(segs, Segment{Kind: SegCommandEnd, ExitCode: code})
		}
		// other OSC 133 variants (A prompt-start, B command-start) are stripped silently
	}
}

// appendOutput appends a SegOutput segment, copying data to avoid aliasing.
func appendOutput(segs []Segment, data []byte) []Segment {
	cp := make([]byte, len(data))
	copy(cp, data)
	return append(segs, Segment{Kind: SegOutput, Data: cp})
}

// partialSuffix returns the index at which haystack ends with a strict prefix
// of needle (length 1…len(needle)-1), or -1 if there is no such suffix.
func partialSuffix(haystack, needle []byte) int {
	for l := len(needle) - 1; l >= 1; l-- {
		if bytes.HasSuffix(haystack, needle[:l]) {
			return len(haystack) - l
		}
	}
	return -1
}
