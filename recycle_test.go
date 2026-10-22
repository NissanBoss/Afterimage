package main

// The recycle bin records, both layouts.
//
// The version 1 layout wrote the path into a fixed field of 260 characters
// and padded the rest, and version 2 put a length in front of it. Reading
// a version 2 file with the version 1 rule gives a path with the next
// record's bytes on the end of it, which looks like a plausible path and
// is not one.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

func recycleRecord(version uint64, size uint64, when uint64, path string) []byte {
	var out []byte
	out = binary.LittleEndian.AppendUint64(out, version)
	out = binary.LittleEndian.AppendUint64(out, size)
	out = binary.LittleEndian.AppendUint64(out, when)
	units := append(utf16.Encode([]rune(path)), 0)
	if version == 1 {
		for len(units) < 260 {
			units = append(units, 0)
		}
		units = units[:260]
	} else {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(units)))
	}
	for _, u := range units {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return out
}

func TestReadsBothRecycleBinLayouts(t *testing.T) {
	const path = `C:\Users\x\Desktop\tax return 2019.pdf`
	for _, version := range []uint64{1, 2} {
		b, ok := parseRecycleRecord(recycleRecord(version, 76543, 133700000000000000, path))
		if !ok {
			t.Fatalf("version %d did not parse", version)
		}
		if b.path != path {
			t.Errorf("version %d gave the path as %q", version, b.path)
		}
		if b.size != 76543 {
			t.Errorf("version %d gave the size as %d", version, b.size)
		}
		if b.deleted.Year() != 2024 {
			t.Errorf("version %d gave the deletion time as %v", version, b.deleted)
		}
	}
}

func TestNonsenseRecycleRecordsAreRefused(t *testing.T) {
	good := recycleRecord(2, 1, 133700000000000000, `C:\a\b.txt`)
	for name, body := range map[string][]byte{
		"empty":            {},
		"header only":      good[:24],
		"cut mid path":     good[:32],
		"unknown version":  recycleRecord(9, 1, 0, `C:\a\b.txt`),
		"absurd path size": append(append([]byte{}, good[:24]...), 0xFF, 0xFF, 0xFF, 0x7F),
	} {
		if _, ok := parseRecycleRecord(body); ok {
			t.Errorf("%s was accepted as a record", name)
		}
	}
}

// The finding that matters here is the $I with no $R: the file is gone
// for good and its full path, size and time of death are still written
// down beside where it used to be.
func TestARecordWithNoFileIsFound(t *testing.T) {
	dir := t.TempDir()
	defer setHomeTo(t, dir)()
	bin := filepath.Join(dir, "$Recycle.Bin", "S-1-5-21-1")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, body []byte) {
		if err := os.WriteFile(filepath.Join(bin, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("$IAAAAAAA.pdf", recycleRecord(2, 100, 133700000000000000, `C:\Users\x\Desktop\still-in-the-bin.pdf`))
	write("$RAAAAAAA.pdf", []byte("the file itself"))
	write("$IBBBBBBB.pdf", recycleRecord(2, 200, 133700000000000000, `C:\Users\x\Desktop\gone-for-good.pdf`))

	// Point the shared state at this bin rather than the machine's, and
	// mark it loaded so that scanRecycleBin does not go and read the real
	// one over the top of it.
	binned_, binGaps, binPath = nil, nil, nil
	readBin(filepath.Join(dir, "$Recycle.Bin"))
	binLoaded = home

	r := &Report{}
	scanRecycleBin(r)
	out := renderTo(r)
	if !strings.Contains(out, "lost its file") {
		t.Errorf("the orphaned record was not reported:\n%s", out)
	}
	if !strings.Contains(out, "1 file is in the recycle bin") {
		t.Errorf("the record with its file beside it was miscounted:\n%s", out)
	}
}

// A sanity check against the real bin, where there is one. It asserts the
// shape rather than the contents: every path a record gives up has to look
// like an absolute Windows path, because a wrong layout produces text that
// is plausible until you look at it.
func TestReadsTheRealRecycleBinWhenThereIsOne(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the recycle bin in this shape is a Windows thing")
	}
	if err := setHome(); err != nil {
		t.Skip(err)
	}
	loadBin()
	if len(binned_) == 0 {
		t.Skip("nothing in the recycle bin on this machine")
	}
	for _, b := range binned_ {
		if !windowsPath(b.path) {
			t.Errorf("a record in the real bin gave the path as %q, which is not an absolute path", b.path)
		}
		if b.deleted.IsZero() {
			t.Errorf("a record in the real bin has no deletion time")
		}
	}
	t.Logf("%d records read out of the real recycle bin, every path absolute", len(binned_))
}
