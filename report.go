package main

// What a finding is, and how it reaches the screen.

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

type Severity int

const (
	// Note is worth knowing.
	Note Severity = iota
	// Tells is what these records say about you rather than about a file:
	// the name of the machine, the address of the card in it, the hours
	// you are at the keyboard, the serial number of a stick you owned.
	// The file is still there. You are the thing that leaked.
	Tells
	// Named is the name and the path of something that is not on the disk
	// any more, sitting in a file nobody prunes.
	Named
	// Copied is worse than Named because it is not a reference. There is
	// a copy of what the file looked like, and somebody can open it.
	Copied
	// Outside is a copy that got out of the place that was holding it: the
	// stick you unplugged, the volume you locked, the share you left. The
	// original is behind something. This is not.
	Outside
)

func (s Severity) tag() string {
	switch s {
	case Outside:
		return "OUTSIDE "
	case Copied:
		return "COPIED  "
	case Named:
		return "NAMED   "
	case Tells:
		return "TELLS   "
	}
	return "NOTE    "
}

type Finding struct {
	Severity Severity
	Title    string
	Lines    []string
	// Advice is what somebody does about it. A finding without one is a
	// finding nobody acts on.
	Advice string
	// Where is the file this came out of. Every finding has one, because
	// the answer to "how do you know that" has to be in the report.
	Where string
}

type Report struct {
	Findings []Finding
	// Gaps are what this run could not read. They print first, because
	// they change how everything under them should be taken.
	Gaps []string
}

func (r *Report) add(f Finding) { r.Findings = append(r.Findings, f) }

func (r *Report) gap(text string) {
	for _, seen := range r.Gaps {
		if seen == text {
			return
		}
	}
	r.Gaps = append(r.Gaps, text)
}

func (r *Report) worst() Severity {
	worst := Note
	for _, f := range r.Findings {
		if f.Severity > worst {
			worst = f.Severity
		}
	}
	return worst
}

const (
	indent = "              "
	wrap   = 62
)

func (r *Report) render(out io.Writer, title string) {
	fmt.Fprintf(out, "\n  %s\n  %s\n\n", title, strings.Repeat("=", min(len(title), wrap+13)))

	for _, g := range r.Gaps {
		block(out, "UNSEEN  ", "Part of this could not be read", nil, g, "")
	}

	// Loudest first. Somebody who reads the first screen and stops should
	// have read the part that mattered.
	sorted := make([]Finding, len(r.Findings))
	copy(sorted, r.Findings)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Severity > sorted[j].Severity
	})
	for _, f := range sorted {
		block(out, f.Severity.tag(), f.Title, f.Lines, f.Advice, f.Where)
	}

	if len(r.Findings) == 0 && len(r.Gaps) == 0 {
		fmt.Fprintln(out, "    Nothing turned up in the places this knows about.")
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, "    Nothing was written, moved or deleted. This only read.")
	fmt.Fprintln(out)
}

func block(out io.Writer, tag, title string, lines []string, advice, where string) {
	// The title carries a file path often enough that it runs off the
	// side, so it wraps under itself. A path with no spaces in it still
	// goes over, and that is the right trade: breaking a path in half so
	// it fits makes it uncopyable.
	head := wrapText(title, wrap+10)
	if len(head) == 0 {
		head = []string{""}
	}
	fmt.Fprintf(out, "    [%s] %s\n", tag, head[0])
	for _, line := range head[1:] {
		fmt.Fprintf(out, "%s%s\n", indent, line)
	}
	for _, line := range lines {
		if line = strings.TrimRight(line, " "); line == "" {
			// A blank separator prints as a blank line and not as
			// fourteen spaces, so nothing here leaves trailing whitespace
			// in something somebody is going to paste elsewhere.
			fmt.Fprintln(out)
			continue
		}
		fmt.Fprintf(out, "%s%s\n", indent, line)
	}
	if where != "" {
		if len(lines) > 0 {
			fmt.Fprintln(out)
		}
		for _, line := range folded("out of ", where) {
			fmt.Fprintf(out, "%s%s\n", indent, line)
		}
	}
	if advice != "" {
		fmt.Fprintln(out)
		for _, line := range wrapText(advice, wrap) {
			fmt.Fprintf(out, "%s%s\n", indent, line)
		}
	}
	fmt.Fprintln(out)
}

// Everything that lines text up counts runes rather than bytes. The
// character this program masks names with is three bytes long, so a column
// padded by byte count drifts left by two spaces for every dot in the row
// above it, and a report full of masked names comes out looking like it
// was assembled by hand in the dark.
func width(s string) int { return utf8.RuneCountInString(s) }

func wrapText(s string, limit int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if width(line)+1+width(w) > limit {
			lines = append(lines, line)
			line = w
			continue
		}
		line += " " + w
	}
	return append(lines, line)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// atMost keeps a list readable. Somebody with four thousand thumbnails
// does not need all four thousand printed to take the point.
func atMost(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	kept := make([]string, 0, n+1)
	kept = append(kept, lines[:n]...)
	return append(kept, fmt.Sprintf("... and %d more", len(lines)-n))
}

// columns lines a list up so the file and the thing found sit under each
// other, because a report with ragged columns gets skimmed rather than
// read. A path too long for its column drops to the next line instead of
// shoving everything sideways.
const columnAt = 34

func columns(left, right string) []string {
	if right == "" {
		return []string{left}
	}
	// The right hand side wraps on its own and the padding is put back on
	// afterwards. Wrapping the finished line instead eats the padding,
	// because wrapping works on words and a run of spaces is not one, so
	// every row long enough to wrap lost its column.
	if width(left) >= columnAt {
		out := []string{left}
		for _, part := range wrapText(right, wrap+6) {
			out = append(out, "    "+part)
		}
		return out
	}
	pad := strings.Repeat(" ", columnAt)
	parts := wrapText(right, wrap+14-columnAt)
	if len(parts) == 0 {
		return []string{left}
	}
	out := []string{left + strings.Repeat(" ", columnAt-width(left)) + parts[0]}
	for _, part := range parts[1:] {
		out = append(out, pad+part)
	}
	return out
}

// folded puts a long sentence under a short label and keeps the second
// line under the first word rather than back at the margin.
func folded(label, text string) []string {
	wrapped := wrapText(text, wrap-len(label))
	out := make([]string, 0, len(wrapped))
	for i, line := range wrapped {
		if i == 0 {
			out = append(out, label+line)
			continue
		}
		out = append(out, strings.Repeat(" ", len(label))+line)
	}
	return out
}

// group is a list of two column rows that remembers how many rows it has,
// which is not the same as how many lines it prints: a path too long for
// its column takes two lines, and counting lines would have the report
// saying twelve files when it means nine.
type group struct {
	lines []string
	count int
	ends  []int // where each row finishes, so the list can be cut by row
}

func (g *group) add(left, right string) {
	g.lines = append(g.lines, columns(left, right)...)
	g.count++
	g.ends = append(g.ends, len(g.lines))
}

func (g *group) addLine(line string) {
	g.lines = append(g.lines, line)
	g.count++
	g.ends = append(g.ends, len(g.lines))
}

// top cuts to the first n rows. Cutting to the first n lines instead had
// the report printing a title that said twenty one files above a list of
// twelve, because every row whose right hand column wrapped was two lines
// and the trailing count was counting the wrong thing.
func (g *group) top(n int) []string {
	if showAll || g.count <= n || n <= 0 || n > len(g.ends) {
		return g.lines
	}
	out := append([]string{}, g.lines[:g.ends[n-1]]...)
	return append(out, fmt.Sprintf("... and %d more", g.count-n))
}
