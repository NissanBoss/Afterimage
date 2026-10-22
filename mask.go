package main

// Covering things over, and the one decision behind it.
//
// This is a report about the files you keep, so the report itself is a
// list of the files you keep. People paste these into issues and forum
// posts and screenshots, which is the whole reason the default is covered.
// The finding survives the masking: forty forgotten files in Downloads is
// the same finding whether or not you can read their names, and --reveal
// is there for the one person who needs to know which forty.

import (
	"path/filepath"
	"strings"
)

var reveal bool

// dots is a fixed width on purpose. One dot per character would hand over
// the length of the name, and the length of a filename is most of the way
// to guessing it when the folder is already on the line above.
const dots = "\u2022\u2022\u2022\u2022\u2022"

// maskName covers a filename and keeps its extension, because the
// extension is half of what the finding is about. A thumbnail of a .jpg
// and a leftover .kdbx are not the same news.
func maskName(name string) string {
	if reveal || name == "" {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	// Two letters of a long name is enough to recognise your own file
	// without being enough to read somebody else's over your shoulder.
	// Short names give up one letter, and names of one or two characters
	// give up nothing, because there would be nothing left to cover.
	keep := 0
	switch {
	case len([]rune(stem)) >= 6:
		keep = 2
	case len([]rune(stem)) >= 3:
		keep = 1
	}
	runes := []rune(stem)
	return string(runes[:keep]) + dots + ext
}

// maskPath keeps the folders and covers the last element. Where a file
// lived is the context that makes the finding readable, and it is far less
// identifying than what it was called.
func maskPath(path string) string {
	if reveal || path == "" {
		return path
	}
	sep := "/"
	if strings.Contains(path, "\\") {
		sep = "\\"
	}
	cut := strings.LastIndex(path, sep)
	if cut < 0 {
		return maskName(path)
	}
	return maskUser(path[:cut+1]) + maskName(path[cut+1:])
}

// maskUser takes the account name out of a path. It is on the front of
// every line in the report and it is the one string that ties the whole
// output to a person.
func maskUser(path string) string {
	if reveal || userName == "" {
		return path
	}
	for _, sep := range []string{"\\", "/"} {
		path = strings.ReplaceAll(path, sep+userName+sep, sep+"<you>"+sep)
		if strings.HasSuffix(path, sep+userName) {
			path = strings.TrimSuffix(path, sep+userName) + sep + "<you>"
		}
	}
	return path
}

// maskHex covers a serial number or a hardware address, which name the
// machine rather than the file and are the reason somebody would want this
// output in the first place.
func maskHex(s string) string {
	if reveal || s == "" {
		return s
	}
	if cut := strings.IndexAny(s, ":-"); cut > 0 {
		// A hardware address gives up its first group. That is the vendor
		// of the card, which is worth seeing and identifies nobody.
		return s[:cut+1] + "**:**:**:**:**"
	}
	return "********"
}
