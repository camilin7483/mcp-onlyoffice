package ooxml

// Fuzz targets for the package readers. Every seed is a real-shaped
// input (a generated package, a truncation, or a minimal archive), never
// random noise. Each function stays cheap by skipping inputs over 1 MiB.

import (
	"strings"
	"testing"
)

func mustSeedDocx() []byte {
	data, err := CreateDOCX("Seed Title")
	if err != nil {
		panic(err)
	}
	return data
}

func mustSeedXlsx() []byte {
	data, err := CreateXLSX("Seed")
	if err != nil {
		panic(err)
	}
	return data
}

// FuzzDigestAndEdit feeds arbitrary bytes through Digest and the paragraph
// edit path: it must never panic, and a successful edit must return a
// package Digest still accepts.
func FuzzDigestAndEdit(f *testing.F) {
	docx := mustSeedDocx()
	xlsx := mustSeedXlsx()
	f.Add(docx)
	f.Add(xlsx)
	f.Add(docx[:len(docx)/2])
	f.Add([]byte("not a package"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 1<<20 {
			t.Skip("input over cheap-fuzz budget")
		}
		d0 := Digest(src)
		if len(d0) != 16 || strings.Trim(d0, "0123456789abcdef") != "" {
			t.Fatalf("Digest = %q, want 16 hex chars", d0)
		}
		out, err := ReplaceParagraphText(src, 0, "x")
		if err != nil {
			return
		}
		d1 := Digest(out)
		if len(d1) != 16 {
			t.Fatalf("Digest(edited) = %q, want 16 hex chars", d1)
		}
		// Must remain readable without panicking; an error is fine.
		_, _ = ParagraphText(out, 0)
	})
}

// FuzzZipParts reads parts with the package's own reader (readEditEntries):
// no panic on any input, and a successful read stays within the named caps
// no matter the input shape.
func FuzzZipParts(f *testing.F) {
	docx := mustSeedDocx()
	f.Add(docx)
	f.Add(mustSeedXlsx())
	f.Add(docx[:len(docx)/4])
	f.Add([]byte("PK\x03\x04garbage-after-local-header"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 1<<20 {
			t.Skip("input over cheap-fuzz budget")
		}
		entries, err := readEditEntries(src)
		if err != nil {
			return
		}
		if len(entries) > maxEditEntries {
			t.Fatalf("entries = %d, over the %d cap", len(entries), maxEditEntries)
		}
		var total int64
		for _, e := range entries {
			if int64(len(e.data)) > maxEditBytes {
				t.Fatalf("part %s = %d bytes, over the %d cap", e.header.Name, len(e.data), maxEditBytes)
			}
			total += int64(len(e.data))
		}
		if total > maxEditTotalBytes {
			t.Fatalf("total = %d bytes, over the %d cap", total, maxEditTotalBytes)
		}
	})
}
