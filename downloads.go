package main

// The download history, which is a list of files with the address each
// one came from.
//
// This is the record people are most surprised by, because clearing your
// browsing history is a thing everybody knows how to do and the downloads
// list is a separate box in the same dialog that plenty of people leave
// unticked. What is in it is not just what you downloaded: it is the page
// you were on when you downloaded it, the size, the type, whether you ever
// opened it, and where on the disk it was put. All of that outlives the
// file by default.
//
// Chromium keeps it in a table called downloads inside History. Firefox
// keeps it as annotations on a place, which is a roundabout way of saying
// there is a table of attribute names and a table of values and the
// download path is a row that points at the row named
// downloads/destinationFileURI.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type download struct {
	path    string
	from    string
	browser string
	bytes   int64
	when    time.Time
}

func scanDownloads(r *Report) {
	var all []download
	for _, db := range chromiumProfiles() {
		all = append(all, readChromiumDownloads(db, r)...)
	}
	for _, db := range firefoxProfiles() {
		all = append(all, readFirefoxDownloads(db, r)...)
	}
	if len(all) == 0 {
		return
	}

	var (
		missing  group
		outside  group
		hosts    = map[string]int{}
		browsers = map[string]int{}
		oldest   time.Time
		unable   int
		recycled int
		gone     int
	)
	sort.Slice(all, func(i, j int) bool { return all[i].when.After(all[j].when) })
	for _, d := range all {
		browsers[d.browser]++
		if !d.when.IsZero() && (oldest.IsZero() || d.when.Before(oldest)) {
			oldest = d.when
		}
		right := maskUser(filepath.Dir(d.path))
		if host := urlHost(d.from); host != "" {
			right += ", from " + maskName(host)
		}
		switch judge(d.path, 0) {
		case nowGone:
			gone++
			missing.add(maskName(filepath.Base(d.path)), right)
			if host := urlHost(d.from); host != "" {
				hosts[host]++
			}
		case inTheBin:
			recycled++
		case offTheMachine:
			outside.add(maskName(filepath.Base(d.path)), right)
		case cannotSay:
			unable++
		}
	}
	if unable > 0 {
		r.gap(plural(unable, "download record points somewhere this run could not check",
			"download records point somewhere this run could not check") + ". None of those are counted as gone.")
	}

	// The name of the browser is not masked. It is a product, not a person,
	// and without it the report cannot say which of two browsers to go and
	// clear.
	var kinds []string
	for _, b := range sorted(browsers) {
		kinds = append(kinds, b+" "+plural(browsers[b], "record", "records"))
	}
	from := "the download history: " + strings.Join(kinds, ", ")

	if missing.count > 0 {
		lines := missing.top(12)
		if recycled > 0 {
			lines = append(lines, "")
			lines = append(lines, wrapText(plural(recycled, "other download is in the recycle bin or trash",
				"other downloads are in the recycle bin or trash")+", so those are not counted here.", wrap+10)...)
		}
		title := plural(gone, "download is still on the record and the file is gone", "downloads are still on the record and the files are gone")
		if !oldest.IsZero() {
			title += ", going back to " + oldest.Format("January 2006")
		}
		r.add(Finding{
			Severity: Named,
			Title:    title,
			Lines:    lines,
			Where:    from,
			Advice: "Each row keeps the name, the folder it went to, the size, and the address of the page " +
				"you were on when you started it. Deleting the file changes none of that, and clearing your " +
				"browsing history does not either unless the download box was ticked as well. " +
				"The list is in the browser under downloads, and it can be cleared from there.",
		})
	}

	if len(hosts) > 0 {
		names := sorted(hosts)
		sort.Slice(names, func(i, j int) bool { return hosts[names[i]] > hosts[names[j]] })
		var g group
		for _, h := range names {
			g.add("  "+maskName(h), plural(hosts[h], "file", "files"))
		}
		lines := g.top(10)
		r.add(Finding{
			Severity: Tells,
			Title: plural(len(hosts), "site is named as the source of a file you no longer have",
				"sites are named as the source of files you no longer have"),
			Lines:  lines,
			Where:  from,
			Advice: "The file is gone and the record of where you got it is not. That is a list of what you were looking for, not just what you kept.",
		})
	}

	if outside.count > 0 {
		lines := outside.top(12)
		r.add(Finding{
			Severity: Outside,
			Title:    plural(outside.count, "download went to a volume that is not mounted", "downloads went to volumes that are not mounted"),
			Lines:    lines,
			Where:    from,
			Advice:   "Downloaded straight onto something removable. The record of it stayed behind.",
		})
	}
}

// chromiumProfiles finds every History database under every Chromium
// family browser, on all three systems, because the folder layout is the
// only thing that differs between them.
func chromiumProfiles() []string {
	var bases []string
	for _, b := range [][]string{
		{"Google", "Chrome", "User Data"},
		{"Google", "Chrome Beta", "User Data"},
		{"Microsoft", "Edge", "User Data"},
		{"BraveSoftware", "Brave-Browser", "User Data"},
		{"Chromium", "User Data"},
		{"Vivaldi", "User Data"},
		{"Opera Software", "Opera Stable"},
	} {
		bases = append(bases, local(b...))
	}
	for _, b := range []string{
		"google-chrome", "chromium", "BraveSoftware/Brave-Browser", "microsoft-edge", "vivaldi", "opera",
	} {
		bases = append(bases, under(".config", filepath.FromSlash(b)))
	}
	for _, b := range [][]string{
		{"Google", "Chrome"}, {"BraveSoftware", "Brave-Browser"}, {"Chromium"}, {"Microsoft Edge"},
	} {
		bases = append(bases, under(append([]string{"Library", "Application Support"}, b...)...))
	}

	var out []string
	for _, base := range bases {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			db := filepath.Join(base, e.Name(), "History")
			if _, err := os.Stat(db); err == nil {
				out = append(out, db)
			}
		}
	}
	return out
}

func firefoxProfiles() []string {
	var bases []string
	bases = append(bases, roaming("Mozilla", "Firefox", "Profiles"))
	bases = append(bases, under(".mozilla", "firefox"))
	bases = append(bases, under("Library", "Application Support", "Firefox", "Profiles"))
	bases = append(bases, under("snap", "firefox", "common", ".mozilla", "firefox"))

	var out []string
	for _, base := range bases {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			db := filepath.Join(base, e.Name(), "places.sqlite")
			if _, err := os.Stat(db); err == nil {
				out = append(out, db)
			}
		}
	}
	return out
}

// browserName takes the name out of the path rather than tracking it
// alongside, so that adding a folder above adds a name for free.
func browserName(path string) string {
	lower := strings.ToLower(filepath.ToSlash(path))
	for _, pair := range [][2]string{
		{"brave", "Brave"}, {"edge", "Edge"}, {"chrome", "Chrome"}, {"chromium", "Chromium"},
		{"vivaldi", "Vivaldi"}, {"opera", "Opera"}, {"firefox", "Firefox"}, {"mozilla", "Firefox"},
	} {
		if strings.Contains(lower, pair[0]) {
			return pair[1]
		}
	}
	return "a browser"
}

func openDatabase(path string, r *Report) *sqlite {
	body, err := os.ReadFile(path)
	if err != nil {
		// A running browser holds the file open on Windows. That is a gap
		// and it is worth naming, because somebody who sees no downloads
		// reported should not conclude there are none.
		r.gap(browserName(path) + " keeps its history in a file this run could not open, which usually " +
			"means the browser is running. Close it and run this again.")
		return nil
	}
	db, err := openSQLite(body)
	if err != nil {
		r.gap("a browser database could not be read: " + err.Error())
		return nil
	}
	return db
}

func readChromiumDownloads(path string, r *Report) []download {
	db := openDatabase(path, r)
	if db == nil {
		return nil
	}
	t, ok := db.tables()["downloads"]
	if !ok {
		return nil
	}
	target := t.column("target_path")
	source := t.column("tab_url")
	total := t.column("total_bytes")
	started := t.column("start_time")
	if target < 0 {
		return nil
	}
	name := browserName(path)

	var out []download
	for _, row := range db.read(t) {
		d := download{browser: name, path: row[target].text}
		if d.path == "" {
			continue
		}
		if source >= 0 {
			d.from = row[source].text
		}
		if total >= 0 {
			d.bytes = row[total].num
		}
		if started >= 0 {
			// Chromium counts microseconds from 1601, the same epoch
			// Windows uses and a different unit, which is exactly the
			// kind of difference that puts every date in the year 1601
			// and looks like a parser bug rather than a unit bug.
			d.when = filetime(uint64(row[started].num) * 10)
		}
		out = append(out, d)
	}
	return out
}

func readFirefoxDownloads(path string, r *Report) []download {
	db := openDatabase(path, r)
	if db == nil {
		return nil
	}
	tables := db.tables()
	attrs, ok := tables["moz_anno_attributes"]
	if !ok {
		return nil
	}
	wanted := int64(-1)
	for _, row := range db.read(attrs) {
		if len(row) < 2 {
			continue
		}
		if row[1].text == "downloads/destinationFileURI" {
			wanted = row[0].num
		}
	}
	annos, ok := tables["moz_annos"]
	if !ok || wanted < 0 {
		return nil
	}
	attrCol := annos.column("anno_attribute_id")
	contentCol := annos.column("content")
	dateCol := annos.column("dateadded")
	if attrCol < 0 || contentCol < 0 {
		return nil
	}
	name := browserName(path)

	var out []download
	for _, row := range db.read(annos) {
		if row[attrCol].num != wanted {
			continue
		}
		d := download{browser: name, path: fileURI(row[contentCol].text)}
		if d.path == "" {
			continue
		}
		if dateCol >= 0 && row[dateCol].num > 0 {
			d.when = time.UnixMicro(row[dateCol].num).UTC()
		}
		out = append(out, d)
	}
	return out
}

func urlHost(raw string) string {
	rest := raw
	if cut := strings.Index(rest, "://"); cut >= 0 {
		rest = rest[cut+3:]
	} else {
		return ""
	}
	if cut := strings.IndexAny(rest, "/?#"); cut >= 0 {
		rest = rest[:cut]
	}
	if cut := strings.LastIndex(rest, "@"); cut >= 0 {
		rest = rest[cut+1:] // a user name in a url is not a host name
	}
	if cut := strings.LastIndex(rest, ":"); cut > 0 && !strings.Contains(rest, "]") {
		rest = rest[:cut]
	}
	return rest
}
