package main

// The recent documents list, which on Linux is a single XML file that
// almost nothing ever prunes.
//
// GTK writes recently-used.xbel and it is not a short list: an entry per
// file, with the full path, when it was added, when it was last opened,
// how many times, and the name of every application that opened it. Which
// makes it the one place on the machine that says you opened this
// particular file with this particular program eleven times.
//
// KDE keeps the same idea as one small file per document in
// RecentDocuments, and those are read here too, because a machine with
// both desktops installed has both lists and clearing one in a settings
// panel leaves the other.

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type recentEntry struct {
	path  string
	apps  []string
	count int
	seen  time.Time
	from  string
}

func scanRecentlyUsed(r *Report) {
	var entries []recentEntry

	share := under(".local", "share")
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		share = x
	}
	for _, name := range []string{
		filepath.Join(share, "recently-used.xbel"),
		under(".recently-used.xbel"),
	} {
		body, err := os.ReadFile(name)
		if err != nil {
			if !os.IsNotExist(err) {
				r.gap("the recent documents list is there and could not be read: " + err.Error())
			}
			continue
		}
		got, err := parseXBEL(body)
		if err != nil {
			r.gap("the recent documents list could not be parsed: " + err.Error())
			continue
		}
		for i := range got {
			got[i].from = name
		}
		entries = append(entries, got...)
	}
	entries = append(entries, kdeRecent(share, r)...)

	if len(entries) == 0 {
		return
	}

	var (
		missing  group
		outside  group
		apps     = map[string]int{}
		oldest   time.Time
		unable   int
		recycled int
	)
	for _, e := range entries {
		for _, a := range e.apps {
			apps[a]++
		}
		if !e.seen.IsZero() && (oldest.IsZero() || e.seen.Before(oldest)) {
			oldest = e.seen
		}
		right := maskUser(filepath.Dir(e.path))
		if len(e.apps) > 0 {
			right += ", last opened with " + maskName(e.apps[len(e.apps)-1])
		}
		switch judge(e.path, 0) {
		case nowGone:
			missing.add(maskName(filepath.Base(e.path)), right)
		case inTheBin:
			recycled++
		case offTheMachine:
			outside.add(maskName(filepath.Base(e.path)), right)
		case cannotSay:
			unable++
		}
	}
	if unable > 0 {
		r.gap(plural(unable, "recent document points somewhere this run could not check",
			"recent documents point somewhere this run could not check") + ". None of those are counted as gone.")
	}

	if missing.count > 0 {
		lines := missing.top(12)
		if recycled > 0 {
			lines = append(lines, "")
			lines = append(lines, wrapText(plural(recycled, "other entry names a file in the trash",
				"other entries name files in the trash")+", so those are not counted here.", wrap+10)...)
		}
		title := plural(missing.count, "recent document is named here and is not on the disk", "recent documents are named here and are not on the disk") + " any more"
		if !oldest.IsZero() {
			title += ", going back to " + oldest.Format("January 2006")
		}
		r.add(Finding{
			Severity: Named,
			Title:    title,
			Lines:    lines,
			Where:    maskUser(filepath.Join(share, "recently-used.xbel")),
			Advice: "The entry holds the full path, the times it was opened and the application that opened " +
				"it. Deleting the file leaves all of that. Most desktops have a Clear button for recent files " +
				"under privacy, and it clears the desktop's own list rather than every list on the machine.",
		})
	}

	if outside.count > 0 {
		lines := outside.top(12)
		r.add(Finding{
			Severity: Outside,
			Title:    plural(outside.count, "recent document is on a volume that is not mounted", "recent documents are on volumes that are not mounted"),
			Lines:    lines,
			Where:    maskUser(filepath.Join(share, "recently-used.xbel")),
			Advice:   "The volume is not here and the list of what was on it is.",
		})
	}

	if len(apps) > 0 && (missing.count > 0 || outside.count > 0) {
		names := sorted(apps)
		sort.Slice(names, func(i, j int) bool { return apps[names[i]] > apps[names[j]] })
		var g group
		for _, name := range names {
			g.add("  "+name, plural(apps[name], "document", "documents"))
		}
		lines := g.top(10)
		r.add(Finding{
			Severity: Tells,
			Title:    plural(len(apps), "application is named in the recent documents list", "applications are named in the recent documents list"),
			Lines:    lines,
			Where:    maskUser(filepath.Join(share, "recently-used.xbel")),
			Advice: "The list records which program opened which file, so it describes what you work on as " +
				"well as what you opened.",
		})
	}
}

// parseXBEL walks the document rather than unmarshalling it into a struct.
// The format puts the application names in a nest of namespaced elements
// that the encoding/xml struct tags handle badly, and a token walk is
// shorter than the tags would be and does not silently drop an entry when
// a desktop writes a namespace prefix nobody expected.
func parseXBEL(body []byte) ([]recentEntry, error) {
	d := xml.NewDecoder(strings.NewReader(string(body)))
	d.Strict = false
	var (
		out     []recentEntry
		current *recentEntry
	)
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch local := t.Name.Local; local {
			case "bookmark":
				current = &recentEntry{}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "href":
						current.path = fileURI(a.Value)
					case "visited", "modified":
						if when, err := time.Parse(time.RFC3339, a.Value); err == nil && current.seen.IsZero() {
							current.seen = when
						}
					}
				}
			case "application":
				if current == nil {
					continue
				}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "name":
						current.apps = append(current.apps, printable(a.Value))
					case "count":
						current.count += atoi(a.Value)
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "bookmark" && current != nil {
				if current.path != "" {
					out = append(out, *current)
				}
				current = nil
			}
		}
	}
	return out, nil
}

func kdeRecent(share string, r *Report) []recentEntry {
	dir := filepath.Join(share, "RecentDocuments")
	files, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			r.gap("the KDE recent documents folder is there and could not be read: " + err.Error())
		}
		return nil
	}
	var out []recentEntry
	for _, f := range files {
		if f.IsDir() || !strings.EqualFold(filepath.Ext(f.Name()), ".desktop") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		e := recentEntry{from: dir}
		for _, line := range strings.Split(string(body), "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok {
				continue
			}
			// The key is written URL[$e] when it holds escapes, so the
			// bracketed part has to come off before comparing.
			key, _, _ = strings.Cut(key, "[")
			switch key {
			case "URL":
				e.path = fileURI(value)
			case "X-KDE-LastOpenedWith", "Name":
				if key == "X-KDE-LastOpenedWith" {
					e.apps = append(e.apps, printable(value))
				}
			}
		}
		if e.path != "" {
			if info, err := f.Info(); err == nil {
				e.seen = info.ModTime()
			}
			out = append(out, e)
		}
	}
	return out
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
