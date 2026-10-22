package main

// The Windows thumbnail cache.
//
// Open a folder of photographs once and Windows keeps a copy of every one
// of them here, at up to 1280 pixels on the long side, which is not a
// thumbnail in any useful sense of the word. Delete the photographs and
// the copies stay. Empty the recycle bin and the copies stay. Nothing in
// normal use ever removes an entry: the file is a ring that is written
// until it is full and then reused, so an image survives until enough
// other images have been looked at to push it out.
//
// The honest limit is here rather than buried at the bottom of the readme.
// Each entry is keyed by a hash of the path, and from Windows 8 onwards
// the identifier string stored beside it is that same hash written out in
// hexadecimal, not the path. So this can say how many pictures are in
// there, how big they are and what they are pictures of in the sense of
// the file format, and it cannot say which file any one of them came from.
// Anything claiming otherwise is reading the search index, which is a
// different file and a different promise.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The fixed part of a cache entry: signature, size, hash, the three
// lengths, the two dimensions, an unused word and two checksums. The
// identifier string starts right after it. Getting this wrong by the
// length of the checksums put the read head in the middle of the picture
// instead of at the front of it, and every single entry came back as an
// unrecognised format rather than as an error, which is the failure mode
// worth remembering about formats made of offsets.
const entryHeader = 0x38

type cacheFile struct {
	name    string
	entries int
	big     int
	bytes   int64
	widest  uint32
	kinds   map[string]int
}

func scanThumbcache(r *Report) {
	dir := local("Microsoft", "Windows", "Explorer")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			r.gap("the thumbnail cache folder is there and could not be read: " + err.Error())
		}
		return
	}

	var (
		files      []*cacheFile
		total      int
		totalBytes int64
		big        int
		widest     uint32
		kinds      = map[string]int{}
	)
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if e.IsDir() || !strings.HasPrefix(name, "thumbcache_") || !strings.HasSuffix(name, ".db") {
			continue
		}
		if strings.Contains(name, "_idx") {
			continue // the index carries no pictures and no paths either
		}
		info, err := e.Info()
		if err != nil || info.Size() < 24 {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			r.gap("a thumbnail cache is there and could not be read: " + err.Error())
			continue
		}
		f := readThumbcache(e.Name(), body)
		if f == nil || f.entries == 0 {
			continue
		}
		files = append(files, f)
		total += f.entries
		totalBytes += f.bytes
		if f.widest > widest {
			widest = f.widest
		}
		for k, n := range f.kinds {
			kinds[k] += n
		}
		// A picture 256 pixels across is a picture. Below that it is a
		// smudge, and the difference is what decides whether somebody
		// pulling this file apart gets to see what your files were. It is
		// counted per picture rather than per file: the 1280 file is named
		// for the largest it will hold, not for what is in it.
		big += f.big
	}

	if total == 0 {
		return
	}

	sort.Slice(files, func(i, j int) bool { return files[i].entries > files[j].entries })
	var g group
	for _, f := range files {
		g.add("  "+f.name, plural(f.entries, "picture", "pictures")+", "+megabytes(f.bytes)+", up to "+size(f.widest))
	}
	var formats []string
	for _, k := range sorted(kinds) {
		formats = append(formats, itoa(kinds[k])+" "+k)
	}

	r.add(Finding{
		Severity: Copied,
		Title:    plural(total, "cached picture is sitting here", "cached pictures are sitting here") + ", " + megabytes(totalBytes) + " of them",
		Lines: append(g.lines, "",
			strings.Repeat(" ", 2)+"formats"+strings.Repeat(" ", columnAt-9)+strings.Join(formats, ", ")),
		Where: maskUser(dir),
		Advice: "These are copies of what your files looked like, and " + plural(big, "of them is", "of them are") +
			" 256 pixels or wider, which is large enough to read a document or recognise a face. " +
			"Deleting the file does not remove its copy from here and neither does emptying the recycle bin. " +
			"What this cannot tell you is which file each one came from: from Windows 8 onwards the entry is keyed " +
			"by a hash of the path and the hash is all that is stored, so the pictures are readable and nameless. " +
			"Disk Cleanup has a Thumbnails box that empties these, and they fill again as soon as you open a folder.",
	})
}

func readThumbcache(name string, body []byte) *cacheFile {
	c := newCursor(body)
	if string(c.take(4)) != "CMMM" {
		return nil
	}
	c.skip(4) // format version
	c.skip(4) // cache type, which is the pixel size in the file name
	c.skip(4)
	first := int(c.u32())
	if c.bad || first < 24 || first > len(body) {
		return nil
	}

	f := &cacheFile{name: name, kinds: map[string]int{}}
	at := first
	for at+entryHeader <= len(body) {
		e := newCursor(body[at:])
		if string(e.take(4)) != "CMMM" {
			break
		}
		size := int(e.u32())
		if size < entryHeader || at+size > len(body) {
			break
		}
		e.skip(8) // entry hash, which is also the identifier string below
		idLen := int(e.u32())
		padding := int(e.u32())
		dataLen := int(e.u32())
		width := e.u32()
		height := e.u32()
		if e.bad {
			break
		}
		if dataLen > 0 {
			// The picture starts after the identifier and its padding.
			// Reading the first bytes of it is the only way to say what
			// these are: the cache holds whatever the shell handed it,
			// which is a JPEG for a photograph and a PNG for anything
			// that needed transparency.
			start := at + entryHeader + idLen + padding
			if start+8 <= len(body) && start+dataLen <= len(body) {
				f.entries++
				f.bytes += int64(dataLen)
				f.kinds[imageKind(body[start:start+8])]++
				if width > f.widest {
					f.widest = width
				}
				if height > f.widest {
					f.widest = height
				}
				if width >= 256 || height >= 256 {
					f.big++
				}
			}
		}
		at += size
	}
	return f
}

func imageKind(head []byte) string {
	switch {
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return "jpeg"
	case len(head) >= 8 && string(head[1:4]) == "PNG":
		return "png"
	case len(head) >= 2 && head[0] == 'B' && head[1] == 'M':
		return "bitmap"
	}
	return "something else"
}

func size(px uint32) string {
	if px == 0 {
		return "an unknown size"
	}
	return itoa(int(px)) + " pixels"
}

func megabytes(n int64) string {
	switch {
	case n >= 1<<20:
		return itoa(int(n>>20)) + " MB"
	case n >= 1<<10:
		return itoa(int(n>>10)) + " KB"
	}
	return itoa(int(n)) + " bytes"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
