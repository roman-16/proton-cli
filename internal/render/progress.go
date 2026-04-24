package render

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// Progress is a minimal TTY-aware progress reporter for byte streams.
// When stderr is not a TTY (or Quiet is true), it emits nothing.
type Progress struct {
	Total  int64
	Label  string
	Writer io.Writer // defaults to os.Stderr
	Quiet  bool

	current int64
	isTTY   bool
	active  bool
}

// Start initialises the progress reporter. Safe to call with zero Total.
func (p *Progress) Start() {
	if p.Writer == nil {
		p.Writer = os.Stderr
	}
	if f, ok := p.Writer.(*os.File); ok {
		p.isTTY = term.IsTerminal(int(f.Fd()))
	}
	p.active = !p.Quiet && p.isTTY
	p.draw()
}

// Add increments progress by n bytes.
func (p *Progress) Add(n int64) {
	p.current += n
	p.draw()
}

// Set sets absolute progress in bytes.
func (p *Progress) Set(n int64) {
	p.current = n
	p.draw()
}

// Finish prints a final newline so subsequent output isn't on the progress line.
func (p *Progress) Finish() {
	if !p.active {
		return
	}
	_, _ = fmt.Fprintln(p.Writer)
	p.active = false
}

func (p *Progress) draw() {
	if !p.active {
		return
	}
	const barWidth = 30
	pct := 0.0
	if p.Total > 0 {
		pct = float64(p.current) / float64(p.Total)
		if pct > 1 {
			pct = 1
		}
	}
	filled := int(pct * float64(barWidth))
	bar := ""
	for i := 0; i < barWidth; i++ {
		switch {
		case i < filled:
			bar += "="
		case i == filled:
			bar += ">"
		default:
			bar += " "
		}
	}
	_, _ = fmt.Fprintf(p.Writer, "\r%s [%s] %s / %s (%.0f%%)",
		p.Label, bar, Size(p.current), Size(p.Total), pct*100)
}
