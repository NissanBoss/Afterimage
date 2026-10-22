package main

// The report itself, which is the part somebody actually reads.
//
// Two of these are here because the bugs they catch were both invisible
// from the code and obvious the moment the output was looked at: a column
// padded by byte count that drifts under masked names, and a list cut to
// twelve lines under a heading that counted twenty one rows.

import (
	"strings"
	"testing"
)

func TestColumnsLineUpUnderMaskedNames(t *testing.T) {
	reveal = false
	var g group
	for _, name := range []string{
		"budget.xlsx",             // plain ascii
		"presupuesto anual.xlsx",  // long enough to be masked heavily
		"a.txt",                   // too short to give anything up
		"niña.png",                // multi byte before masking
		"a-very-long-name-indeed", // no extension at all
	} {
		g.add(maskName(name), "somewhere")
	}
	at := -1
	for _, line := range g.lines {
		cut := strings.Index(line, "somewhere")
		if cut < 0 {
			t.Fatalf("a row lost its right hand side: %q", line)
		}
		// The column is measured in characters, which is what a terminal
		// puts on the screen, not in bytes.
		got := width(line[:cut])
		if at == -1 {
			at = got
		}
		if got != at {
			t.Errorf("rows start their second column at %d and this one at %d: %q", at, got, line)
		}
	}
	if at != columnAt {
		t.Errorf("the second column is at %d and it should be %d", at, columnAt)
	}
}

func TestTheListIsCutByRowAndNotByLine(t *testing.T) {
	showAll = false
	defer func() { showAll = false }()
	var g group
	for i := 0; i < 20; i++ {
		// A right hand side long enough to wrap, so every row is two lines.
		g.add("name"+itoa(i)+".txt", strings.Repeat("a folder that is long enough to wrap ", 2))
	}
	lines := g.top(5)
	tail := lines[len(lines)-1]
	if want := "... and 15 more"; tail != want {
		t.Fatalf("the last line is %q and it should be %q", tail, want)
	}
	for i := 5; i < 20; i++ {
		if strings.Contains(strings.Join(lines, "\n"), "name"+itoa(i)+".txt") {
			t.Errorf("row %d was printed even though the list was cut to five", i)
		}
	}
	for i := 0; i < 5; i++ {
		if !strings.Contains(strings.Join(lines, "\n"), "name"+itoa(i)+".txt") {
			t.Errorf("row %d was cut even though the list keeps five", i)
		}
	}
}

func TestShowAllPrintsEveryRow(t *testing.T) {
	showAll = true
	defer func() { showAll = false }()
	var g group
	for i := 0; i < 40; i++ {
		g.add("name"+itoa(i), "x")
	}
	if got := len(g.top(5)); got != 40 {
		t.Errorf("--all printed %d rows out of 40", got)
	}
}

// Nothing in the report may leave trailing whitespace, because these get
// pasted into issues and diffs where it shows up as red.
func TestNoLineEndsInSpaces(t *testing.T) {
	r := &Report{}
	r.gap("something could not be read")
	r.add(Finding{
		Severity: Outside,
		Title:    "a title long enough that it has to wrap somewhere around about here and then keep going",
		Lines:    []string{"", "a row", ""},
		Advice:   "some advice that is also long enough to wrap onto a second line and probably a third one too",
		Where:    "/a/path/that/is/quite/long/and/wraps/onto/the/next/line/as/well/somewhere",
	})
	for _, line := range strings.Split(renderTo(r), "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("a line ends in whitespace: %q", line)
		}
	}
}

func TestTheLoudestFindingIsPrintedFirst(t *testing.T) {
	r := &Report{}
	r.add(Finding{Severity: Note, Title: "a note"})
	r.add(Finding{Severity: Outside, Title: "off the machine"})
	r.add(Finding{Severity: Named, Title: "named only"})
	out := renderTo(r)
	first := strings.Index(out, "off the machine")
	middle := strings.Index(out, "named only")
	last := strings.Index(out, "a note")
	if !(first < middle && middle < last) {
		t.Errorf("the findings came out in the wrong order:\n%s", out)
	}
}

func TestShortNamesGiveNothingAway(t *testing.T) {
	reveal = false
	for _, name := range []string{"a", "ab", "x.c"} {
		got := maskName(name)
		for _, r := range name {
			if r == '.' {
				continue
			}
			if strings.ContainsRune(got, r) && width(name) <= 2 {
				t.Errorf("%q came back as %q, which gives away a letter of a two character name", name, got)
			}
		}
	}
	if got := maskName("holiday photos.jpg"); !strings.HasSuffix(got, ".jpg") {
		t.Errorf("the extension was covered over as well: %q", got)
	}
	if got := maskName("holiday photos.jpg"); !strings.HasPrefix(got, "ho") {
		t.Errorf("a long name gave up fewer than two letters: %q", got)
	}
}
