package main

// Jump lists: a second set of recent files, kept per application.
//
// Right click an icon on the taskbar and the list that comes up is this.
// It is a separate record from the Recent folder, written by a different
// part of Windows, and every entry in it is a whole shell link with the
// path, the volume and the tracker block in it.
//
// The automatic ones are compound files with one stream per entry. The
// custom ones, the lists an application builds for itself, are a plain
// run of shell links with a header nobody has fully written up, so those
// are found by their signature rather than by walking a structure that
// would have to be guessed at.
//
// The application is named only by a hash of the path of its executable,
// and this does not turn that back into a name. The published tables of
// those hashes are collected by hand and get things wrong, and a report
// that says Notepad when it means something else is worse than one that
// says it does not know.

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The first twenty bytes of every shell link: the header size and the
// class id. Nothing else in these files begins with it.
var linkSignature = []byte{
	0x4C, 0x00, 0x00, 0x00, 0x01, 0x14, 0x02, 0x00, 0x00, 0x00,
	0x00, 0x00, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46,
}

type appList struct {
	id      string
	kind    string
	total   int
	missing group
	outside group
}

func scanJumpLists(r *Report) {
	base := roaming("Microsoft", "Windows", "Recent")
	lists := map[string]*appList{}
	unchecked := 0

	for _, spec := range []struct{ dir, ext, kind string }{
		{filepath.Join(base, "AutomaticDestinations"), ".automaticdestinations-ms", "taskbar"},
		{filepath.Join(base, "CustomDestinations"), ".customdestinations-ms", "the application's own"},
	} {
		entries, err := os.ReadDir(spec.dir)
		if err != nil {
			if !os.IsNotExist(err) {
				r.gap("a jump list folder is there and could not be read: " + err.Error())
			}
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), spec.ext) {
				continue
			}
			body, err := os.ReadFile(filepath.Join(spec.dir, e.Name()))
			if err != nil {
				r.gap("a jump list could not be read: " + err.Error())
				continue
			}
			id := strings.SplitN(e.Name(), ".", 2)[0]
			list := &appList{id: id, kind: spec.kind}
			var links []*Link
			if spec.kind == "taskbar" {
				links = linksFromCompound(body, r, e.Name())
			} else {
				links = linksFromRun(body)
			}
			for _, l := range links {
				list.total++
				unchecked += sortLink(l, "", &list.missing, &list.outside)
			}
			if list.total > 0 {
				lists[id+" "+spec.kind] = list
			}
		}
	}

	if len(lists) == 0 {
		return
	}

	var (
		missingTotal, outsideTotal, entryTotal int
		rows                                   group
		outsideRows                            group
	)
	shownOutside := map[string]bool{}
	keys := make([]string, 0, len(lists))
	for k := range lists {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return lists[keys[i]].missing.count > lists[keys[j]].missing.count })

	for _, k := range keys {
		l := lists[k]
		entryTotal += l.total
		missingTotal += l.missing.count
		if l.missing.count > 0 {
			// The application id is never masked. It is a hash of the path
			// of an executable, so it says nothing about the person, and
			// without it the row is six identical lines saying that some
			// application or other has forgotten something.
			rows.add("  "+l.id, plural(l.missing.count, "gone", "gone")+" of "+plural(l.total, "entry", "entries")+", "+l.kind+" list")
		}
		for _, line := range l.outside.lines {
			// The same file shows up in several lists, because the taskbar
			// keeps one per application and a file opened by two of them is
			// in both. Counting it twice would say there are two things on
			// that unplugged drive when there is one.
			if shownOutside[line] {
				continue
			}
			shownOutside[line] = true
			outsideRows.addLine(line)
			outsideTotal++
		}
	}

	if unchecked > 0 {
		r.gap(plural(unchecked, "jump list entry points somewhere this run could not check",
			"jump list entries point somewhere this run could not check") + ". None of those are counted as gone.")
	}

	if missingTotal > 0 {
		lines := rows.top(12)
		r.add(Finding{
			Severity: Named,
			Title: plural(missingTotal, "jump list entry names a file that is not on the disk", "jump list entries name files that are not on the disk") +
				" any more, across " + plural(rows.count, "application", "applications"),
			Lines: lines,
			Where: maskUser(base) + "\\AutomaticDestinations and \\CustomDestinations",
			Advice: "This is a separate record from the Recent folder, one list per application, and each " +
				"entry is a whole shortcut with the path and the volume in it. Deleting the file does not touch it " +
				"and neither does emptying the recycle bin. Right click the item in the taskbar list and Remove " +
				"from this list takes out one; clearing File Explorer history takes out the automatic ones.",
		})
	}

	if outsideTotal > 0 {
		lines := outsideRows.top(12)
		r.add(Finding{
			Severity: Outside,
			Title:    plural(outsideTotal, "jump list entry describes a file on a drive that is not here", "jump list entries describe files on drives that are not here"),
			Lines:    lines,
			Where:    maskUser(base) + "\\AutomaticDestinations",
			Advice:   "The drive is gone and the description of what was on it is still here.",
		})
	}

	if entryTotal > 0 && missingTotal == 0 && outsideTotal == 0 {
		r.add(Finding{
			Severity: Note,
			Title:    plural(entryTotal, "jump list entry", "jump list entries") + " across " + plural(len(lists), "application", "applications") + ", all still on the disk",
			Where:    maskUser(base),
		})
	}
}

// sortLink puts one record into the right pile and says whether it could
// not be judged at all. The three piles are kept apart everywhere in this
// program: a file that is gone, a file behind something that is not here,
// and a file this run had no way of asking about.
func sortLink(l *Link, name string, missing, outside *group) int {
	target := l.Where()
	if target == "" || l.NetPath != "" && l.Path == "" {
		return 0
	}
	if name == "" {
		name = filepath.Base(target)
	}
	switch judge(target, l.DriveType) {
	case nowGone:
		missing.add(maskName(name), maskUser(filepath.Dir(target)))
	case offTheMachine:
		label := l.Label
		if label == "" {
			label = "no label"
		}
		outside.add(maskName(name), maskUser(filepath.Dir(target))+", "+label)
	case cannotSay:
		return 1
	}
	return 0
}

func linksFromCompound(body []byte, r *Report, name string) []*Link {
	c, err := openCompound(body)
	if err != nil {
		r.gap("a jump list could not be opened as a compound file: " + err.Error())
		return nil
	}
	var out []*Link
	for _, e := range c.entries {
		if e.Kind != 2 || strings.EqualFold(e.Name, "DestList") {
			continue
		}
		stream := c.stream(e)
		if len(stream) == 0 {
			continue
		}
		if l, err := parseLink(stream); err == nil {
			out = append(out, l)
		}
	}
	return out
}

// linksFromRun walks a file looking for the start of a shell link and
// parsing from there. The parser stops at the end of the link it is
// reading, so overlapping matches cannot happen and the next search starts
// after the signature that was just used.
func linksFromRun(body []byte) []*Link {
	var out []*Link
	for at := 0; at < len(body); {
		found := bytes.Index(body[at:], linkSignature)
		if found < 0 {
			break
		}
		start := at + found
		if l, err := parseLink(body[start:]); err == nil {
			out = append(out, l)
		}
		at = start + len(linkSignature)
	}
	return out
}
