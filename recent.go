package main

// The Recent folder: one shortcut per file you have ever opened.
//
// Nothing prunes it. The shortcut is named after the file, so the folder
// listing on its own is a plain text list of what you have opened, and
// that survives the file by years. Inside each one is the full path, the
// size the file was, the three timestamps it had, and the serial number
// and label of the volume it was on, which is how a stick you plugged in
// once and threw away is still described here.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type volume struct {
	serial    uint32
	label     string
	driveType uint32
	seen      int
}

func scanRecent(r *Report) {
	dir := roaming("Microsoft", "Windows", "Recent")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			r.gap("the Recent folder is there and could not be read: " + err.Error())
		}
		return
	}

	var (
		missing     group
		outside     group
		shares      = map[string]int{}
		volumes     = map[uint32]*volume{}
		machines    = map[string]int{}
		macs        = map[string]int{}
		unchecked   int
		recoverable int
		total       int
		oldest      time.Time
		newest      time.Time
		unreadable  int
	)

	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".lnk") {
			continue
		}
		total++
		full := filepath.Join(dir, e.Name())
		l, err := readLink(full)
		if err != nil {
			unreadable++
			continue
		}
		// The modification time of the shortcut is when the file was last
		// opened, which is a fact about you rather than about the file and
		// is not in the shortcut itself.
		if info, err := e.Info(); err == nil {
			when := info.ModTime()
			if oldest.IsZero() || when.Before(oldest) {
				oldest = when
			}
			if when.After(newest) {
				newest = when
			}
		}

		if l.Serial != 0 {
			v := volumes[l.Serial]
			if v == nil {
				v = &volume{serial: l.Serial, label: l.Label, driveType: l.DriveType}
				volumes[l.Serial] = v
			}
			v.seen++
		}
		if l.Machine != "" {
			machines[l.Machine]++
		}
		if l.MAC != "" {
			macs[l.MAC]++
		}
		if l.BirthMAC != "" && l.BirthMAC != l.MAC {
			macs[l.BirthMAC]++
		}

		// The name of the shortcut is the name of the file, so it is the
		// thing worth showing even when the record inside is unreadable.
		shown := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		target := l.Where()

		if l.NetPath != "" && l.Path == "" {
			if host := shareHost(l.NetPath); host != "" {
				shares[host]++
			}
			continue
		}
		if target == "" {
			continue
		}
		switch judge(target, l.DriveType) {
		case nowGone:
			missing.add(maskName(shown), maskUser(filepath.Dir(target)))
		case inTheBin:
			recoverable++
		case offTheMachine:
			// A drive that is not plugged in is the interesting half of
			// this, because the record is on the machine and the file is
			// not, which is the definition of the thing being looked for.
			label := l.Label
			if label == "" {
				label = "no label"
			}
			outside.add(maskName(shown), maskUser(filepath.Dir(target))+", "+label)
		case cannotSay:
			unchecked++
		}
	}

	if total == 0 {
		return
	}
	if unreadable > 0 {
		r.gap(plural(unreadable, "file in the Recent folder is not a readable shortcut", "files in the Recent folder are not readable shortcuts") +
			", so whatever they point at was not checked")
	}
	if unchecked > 0 {
		r.gap(plural(unchecked, "record in the Recent folder points somewhere this run could not check",
			"records in the Recent folder point somewhere this run could not check") +
			", usually a drive that is not mounted on the machine doing the reading. None of those are counted as gone.")
	}

	if missing.count > 0 {
		lines := missing.top(12)
		if recoverable > 0 {
			lines = append(lines, "", plural(recoverable, "other record points at a file that is in the recycle bin",
				"other records point at files that are in the recycle bin")+", so those are not counted here.")
		}
		r.add(Finding{
			Severity: Named,
			Title:    plural(missing.count, "file is named in Recent and is not on the disk", "files are named in Recent and are not on the disk") + " any more",
			Lines:    lines,
			Where:    maskUser(dir),
			Advice: "The name of each shortcut is the name of the file, in the clear, " +
				"and the record inside holds the folder it was in, the size it was and when you opened it. " +
				"Deleting the file does not touch any of that. " +
				"File Explorer, Privacy and security, Clear browsing data for File Explorer empties the folder, " +
				"and turning off recent files under Personalisation, Start stops it filling up again.",
		})
	}

	if outside.count > 0 {
		lines := outside.top(12)
		r.add(Finding{
			Severity: Outside,
			Title:    plural(outside.count, "record describes a file on a drive that is not here", "records describe files on drives that are not here"),
			Lines:    lines,
			Where:    maskUser(dir),
			Advice: "These are the ones that outlive the hardware. The disk they name is " +
				"unplugged, thrown away or lent to somebody, and the machine still has the name of the file, " +
				"the folder it sat in and the label and serial number of the volume it was on.",
		})
	}

	if sticks := removableVolumes(volumes); len(sticks) > 0 {
		var g group
		for _, v := range sticks {
			label := v.label
			if label == "" {
				label = "no label"
			}
			g.add("  "+label, "serial "+maskHex(serialString(v.serial))+", "+plural(v.seen, "record", "records"))
		}
		r.add(Finding{
			Severity: Tells,
			Title:    plural(len(sticks), "removable volume you have plugged in is described here", "removable volumes you have plugged in are described here"),
			Lines:    g.lines,
			Where:    maskUser(dir),
			Advice: "A label and a serial number together identify one particular stick or drive. " +
				"That is enough to match this machine against another machine the same stick was plugged into, " +
				"and it stays here after the stick is gone.",
		})
	}

	if len(macs) > 0 || len(machines) > 0 {
		var g group
		for _, name := range sorted(machines) {
			g.add("  machine name", maskName(name)+"  ("+plural(machines[name], "record", "records")+")")
		}
		for _, mac := range sorted(macs) {
			g.add("  hardware address", maskHex(mac)+"  ("+plural(macs[mac], "record", "records")+")")
		}
		r.add(Finding{
			Severity: Tells,
			Title:    "Every shortcut in Recent carries the name of this machine, and most carry the address of a card in it",
			Lines:    g.lines,
			Where:    maskUser(dir),
			Advice: "Windows puts a tracker block on the end of a shortcut so it can find the file again " +
				"if you move it. The two identifiers in that block are version 1 UUIDs, which are built from a " +
				"timestamp and the hardware address of a network card. Send somebody a shortcut, or a folder with " +
				"one in it, and you send them that. It is the property that put a name to the author of the Melissa " +
				"document in 1999 and it has not changed since.",
		})
	}

	if len(shares) > 0 {
		var g group
		for _, host := range sorted(shares) {
			g.add("  "+maskName(host), plural(shares[host], "record", "records"))
		}
		r.add(Finding{
			Severity: Tells,
			Title:    plural(len(shares), "file server you have opened a file from is named here", "file servers you have opened files from are named here"),
			Lines:    g.lines,
			Where:    maskUser(dir),
			Advice: "These were not checked. Asking whether the file is still there would mean opening a " +
				"connection to the server, and this program does not open any.",
		})
	}

	if !oldest.IsZero() && total > 0 {
		r.add(Finding{
			Severity: Note,
			Title: plural(total, "shortcut in Recent", "shortcuts in Recent") + ", the oldest from " +
				oldest.Format("2 January 2006"),
			Lines: []string{
				"  first record" + strings.Repeat(" ", columnAt-15) + oldest.Format("2 January 2006, 15:04"),
				"  last record" + strings.Repeat(" ", columnAt-14) + newest.Format("2 January 2006, 15:04"),
			},
			Where:  maskUser(dir),
			Advice: "The span is how far back this account can be read, and the times of day are the hours you are at the keyboard.",
		})
	}
}

func removableVolumes(volumes map[uint32]*volume) []*volume {
	var out []*volume
	for _, v := range volumes {
		if v.driveType == 2 || v.driveType == 5 {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seen > out[j].seen })
	return out
}

func serialString(serial uint32) string {
	const hex = "0123456789ABCDEF"
	out := make([]byte, 0, 9)
	for shift := 28; shift >= 0; shift -= 4 {
		out = append(out, hex[(serial>>uint(shift))&0xF])
		if shift == 16 {
			out = append(out, '-')
		}
	}
	return string(out)
}

func shareHost(path string) string {
	trimmed := strings.TrimLeft(path, "\\")
	if cut := strings.IndexByte(trimmed, '\\'); cut > 0 {
		return trimmed[:cut]
	}
	return trimmed
}

func sorted[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
