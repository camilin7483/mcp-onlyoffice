package operations

// FuzzDecodeOperation feeds arbitrary bytes through the strict operation
// decoder: it must never panic, an accepted operation always has a non-empty
// name and a non-zero target, and a rejection never echoes a param value.

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustSeedOperation(name, target string) []byte {
	raw, err := json.Marshal(map[string]any{"operation": name, "target": map[string]any{"document_id": target}})
	if err != nil {
		panic(err)
	}
	return raw
}

// stringVals collects every JSON string value of v recursively.
func stringVals(v any, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for _, c := range t {
			stringVals(c, out)
		}
	case []any:
		for _, c := range t {
			stringVals(c, out)
		}
	case string:
		*out = append(*out, t)
	}
}

func FuzzDecodeOperation(f *testing.F) {
	f.Add(mustSeedOperation("writer.replace_range", "doc1"))
	f.Add(mustSeedOperation("o.p", "d"))
	f.Add([]byte(`{"operation":"writer.replace_range","target":{"document_id":"d"},"params":{"ok_key":"SENTINEL"},"dry_run":true}`))
	f.Add([]byte(`{"operation":"","target":{}}`))
	f.Add([]byte(`{"operation":"o.p","target":{"document_id":"d"},"params":{"bad-key":"x"}}`))
	f.Add([]byte(`{"operation":`))
	f.Add([]byte{})
	f.Add([]byte("not json at all \x00\xff"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxOperationBytes+1024 {
			t.Skip("input over cheap-fuzz budget")
		}
		op, err := DecodeOperation(data)
		if err != nil {
			// Rejections name the field and never echo values: any string
			// value of 8+ chars present in the input must not leak into it.
			var vals []string
			var v any
			if json.Unmarshal(data, &v) == nil {
				stringVals(v, &vals)
			}
			for _, s := range vals {
				if len(s) >= 8 && strings.Contains(err.Error(), s) {
					t.Fatalf("rejection echoes a param value %q in %q", s, err)
				}
			}
			return
		}
		if op.Name == "" {
			t.Fatal("accepted operation has an empty name")
		}
		if op.Target == (Target{}) {
			t.Fatal("accepted operation has a zero target")
		}
	})
}
