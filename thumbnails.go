package main

// The freedesktop thumbnail cache, which is the strongest case this
// program has.
//
// On Linux the thumbnail is a PNG in ~/.cache/thumbnails and the standard
// says it must carry a text chunk called Thumb::URI holding the address of
// the file it was made from. So unlike the Windows cache, which is keyed
// by a hash and can only be counted, this one names its own originals.
//
// That turns a pile of pictures into a list: for every file you have ever
// looked at in a file manager, there is a picture of it here with the full
// path written inside, and nothing removes it when the file goes. Point a
// file manager at a folder on a USB stick once and the pictures of what
// was on that stick stay on the internal disk after you pull it out.
//
// There is a second directory called fail, which holds a zero pixel PNG
// for every file a thumbnailer choked on. Those have no picture in them
// and they still have the path, which makes them a list of files with no
// pictures rather than nothing at all.

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type thumb struct {
	path  string
	size  string
	when  time.Time
	dir   string
	blank bool
}

func scanThumbnails(r *Report) {
	roots := []string{
		under(".cache", "thumbnails"),
		under(".thumbnails"), // where they lived before the cache directory spec
	}
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		roots = append([]string{filepath.Join(x, "thumbnails")}, roots...)
	}

	var (
		found     []thumb
		unreadble int
		noURI     int
		seen      = map[string]bool{}
	)
	for _, root := range roots {
		sizes, err := os.ReadDir(root)
		if err != nil {
			if !os.IsNotExist(err) {
				r.gap("the thumbnail cache is there and could not be read: " + err.Error())
			}
			continue
		}
		for _, s := range sizes {
			if !s.IsDir() {
				continue
			}
			dir := filepath.Join(root, s.Name())
			// The fail directory has one subdirectory per thumbnailer, so
			// it is one level deeper than the rest.
			levels := []string{dir}
			if strings.EqualFold(s.Name(), "fail") {
				levels = nil
				if inner, err := os.ReadDir(dir); err == nil {
					for _, i := range inner {
						if i.IsDir() {
							levels = append(levels, filepath.Join(dir, i.Name()))
						}
					}
				}
			}
			for _, level := range levels {
				files, err := os.ReadDir(level)
				if err != nil {
					r.gap("part of the thumbnail cache could not be read: " + err.Error())
					continue
				}
				for _, f := range files {
					if f.IsDir() || !strings.EqualFold(filepath.Ext(f.Name()), ".png") {
						continue
					}
					body, err := os.ReadFile(filepath.Join(level, f.Name()))
					if err != nil {
						unreadble++
						continue
					}
					text := pngText(body)
					path := fileURI(text["Thumb::URI"])
					if path == "" {
						noURI++
						continue
					}
					if seen[path+level] {
						continue
					}
					seen[path+level] = true
					t := thumb{path: path, dir: s.Name(), blank: strings.EqualFold(s.Name(), "fail")}
					if info, err := f.Info(); err == nil {
						t.when = info.ModTime()
					}
					if w, h := text["Thumb::Image::Width"], text["Thumb::Image::Height"]; w != "" && h != "" {
						t.size = w + " by " + h
					}
					found = append(found, t)
				}
			}
		}
	}

	if len(found) == 0 {
		return
	}
	if unreadble > 0 {
		r.gap(plural(unreadble, "thumbnail could not be read", "thumbnails could not be read") + ", so what it was of is unknown")
	}
	if noURI > 0 {
		r.gap(plural(noURI, "thumbnail has no address written in it", "thumbnails have no address written in them") +
			", so those cannot be matched against the disk. They are still pictures of something.")
	}

	var (
		missing  group
		outside  group
		failed   group
		oldest   time.Time
		unable   int
		recycled int
	)
	for _, t := range found {
		if !t.when.IsZero() && (oldest.IsZero() || t.when.Before(oldest)) {
			oldest = t.when
		}
		right := maskUser(filepath.Dir(t.path))
		if t.size != "" {
			right += ", " + t.size
		}
		switch judge(t.path, 0) {
		case nowGone:
			if t.blank {
				failed.add(maskName(filepath.Base(t.path)), maskUser(filepath.Dir(t.path)))
				continue
			}
			missing.add(maskName(filepath.Base(t.path)), right)
		case inTheBin:
			recycled++
		case offTheMachine:
			outside.add(maskName(filepath.Base(t.path)), right)
		case cannotSay:
			unable++
		}
	}
	if unable > 0 {
		r.gap(plural(unable, "thumbnail names a file this run could not check", "thumbnails name files this run could not check") +
			". None of those are counted as gone.")
	}

	if missing.count > 0 {
		lines := missing.top(12)
		if recycled > 0 {
			lines = append(lines, "")
			lines = append(lines, wrapText(plural(recycled, "other thumbnail is of a file in the recycle bin",
				"other thumbnails are of files in the recycle bin")+", so those are not counted here.", wrap+10)...)
		}
		title := plural(missing.count, "picture is here of a file that is not on the disk", "pictures are here of files that are not on the disk") + " any more"
		if !oldest.IsZero() {
			title += ", the oldest from " + oldest.Format("January 2006")
		}
		r.add(Finding{
			Severity: Copied,
			Title:    title,
			Lines:    lines,
			Where:    maskUser(under(".cache", "thumbnails")),
			Advice: "Each of these is a PNG you can open, and the path of the original is written inside it " +
				"in a text chunk, which is how this knows what it was. Nothing removes them when the file goes: " +
				"not deleting it, not emptying the trash, not the file manager. Emptying the folder is safe and " +
				"they rebuild themselves when you next open a folder.",
		})
	}

	if outside.count > 0 {
		lines := outside.top(12)
		r.add(Finding{
			Severity: Outside,
			Title:    plural(outside.count, "picture is of a file on a volume that is not mounted", "pictures are of files on volumes that are not mounted"),
			Lines:    lines,
			Where:    maskUser(under(".cache", "thumbnails")),
			Advice: "The thumbnail is on the internal disk and the file is on something you unplugged, " +
				"locked or unmounted. This is the case the caches are worst at: opening an encrypted volume in a " +
				"file manager once is enough to leave pictures of what was inside it out here in the clear.",
		})
	}

	if failed.count > 0 {
		lines := failed.top(8)
		r.add(Finding{
			Severity: Named,
			Title:    plural(failed.count, "file that could not be thumbnailed is named here and is gone", "files that could not be thumbnailed are named here and are gone"),
			Lines:    lines,
			Where:    maskUser(under(".cache", "thumbnails", "fail")),
			Advice: "The fail directory keeps an empty PNG for anything a thumbnailer could not draw, so that " +
				"it does not try again. There is no picture in these. There is still the full path of the file.",
		})
	}
}
