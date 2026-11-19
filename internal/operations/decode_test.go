package operations

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func decodeOK(t *testing.T, payload string) Operation {
	t.Helper()
	op, err := DecodeOperation([]byte(payload))
	if err != nil {
		t.Fatalf("DecodeOperation(%s): %v", payload, err)
	}
	return op
}

func TestDecodeValidMinimal(t *testing.T) {
	op := decodeOK(t, `{"operation":"writer.replace_range","target":{"document_id":"doc1"}}`)
	if op.Name != "writer.replace_range" || op.Target.DocumentID != "doc1" {
		t.Fatalf("unexpected decode: %+v", op)
	}
	op = decodeOK(t, `{"operation":"reader.get","target":{"path":"/tmp/x.docx"},"params":{"a_b2":1},"dry_run":true}`)
	if !op.DryRun || op.Params["a_b2"] != float64(1) {
		t.Fatalf("unexpected decode: %+v", op)
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string // field the error must name
	}{
		{"unknown field", `{"operation":"writer.replace_range","target":{"document_id":"d"},"bogus":1}`, "bogus"},
		{"unknown nested field", `{"operation":"writer.replace_range","target":{"document_id":"d","nope":1}}`, "nope"},
		{"empty name", `{"operation":"","target":{"document_id":"d"}}`, "operation"},
		{"missing name", `{"target":{"document_id":"d"}}`, "operation"},
		{"empty target", `{"operation":"writer.replace_range","target":{}}`, "target"},
		{"missing target", `{"operation":"writer.replace_range"}`, "target"},
		{"bad param key dash", `{"operation":"o.p","target":{"document_id":"d"},"params":{"bad-key":1}}`, "params"},
		{"bad param key upper", `{"operation":"o.p","target":{"document_id":"d"},"params":{"Bad":1}}`, "params"},
		{"bad param key empty", `{"operation":"o.p","target":{"document_id":"d"},"params":{"":1}}`, "params"},
		{"paragraph+range", `{"operation":"o.p","target":{"document_id":"d","paragraph":1,"range_start":0}}`, "target"},
		{"paragraph+rangeEnd", `{"operation":"o.p","target":{"document_id":"d","paragraph":1,"range_end":2}}`, "target"},
		{"range order", `{"operation":"o.p","target":{"document_id":"d","range_start":5,"range_end":2}}`, "target"},
		{"trailing data", `{"operation":"o.p","target":{"document_id":"d"}} garbage`, "operation"},
		{"malformed", `{"operation":`, "operation"},
	}
	for _, c := range cases {
		_, err := DecodeOperation([]byte(c.payload))
		if err == nil {
			t.Errorf("%s: expected error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q must name field %q", c.name, err, c.want)
		}
	}
}

func TestDecodeOversized(t *testing.T) {
	big := `{"operation":"o.p","target":{"document_id":"` + strings.Repeat("x", MaxOperationBytes) + `"}}`
	if _, err := DecodeOperation([]byte(big)); err == nil {
		t.Fatal("oversized input must fail")
	} else if !strings.Contains(err.Error(), "operation") {
		t.Fatalf("oversized error must name a field: %v", err)
	}
}

func TestDecodeTooManyParams(t *testing.T) {
	params := map[string]any{}
	for i := 0; i < MaxParamsKeys+1; i++ {
		params[fmt.Sprintf("k_%d", i)] = i
	}
	raw, _ := json.Marshal(map[string]any{"operation": "o.p", "target": map[string]any{"document_id": "d"}, "params": params})
	_, err := DecodeOperation(raw)
	if err == nil || !strings.Contains(err.Error(), "params") {
		t.Fatalf("too many params must fail naming params: %v", err)
	}
}

func TestDecodeNeverEchoesParamValues(t *testing.T) {
	const sentinel = "SENTINEL_NO_ECHO_9f8c7b"
	payloads := []string{
		`{"operation":"","target":{"document_id":"d"},"params":{"ok_key":"` + sentinel + `"}}`,
		`{"operation":"o.p","target":{},"params":{"ok_key":"` + sentinel + `"}}`,
		`{"operation":"o.p","target":{"document_id":"d"},"params":{"bad-key":"` + sentinel + `"}}`,
		`{"operation":"o.p","target":{"document_id":"d","paragraph":1,"range_start":0},"params":{"ok_key":"` + sentinel + `"}}`,
		`{"operation":"o.p","target":{"document_id":"d","range_start":9,"range_end":1},"params":{"ok_key":"` + sentinel + `"}}`,
		`{"operation":"o.p","target":{"document_id":"d","paragraph":"` + sentinel + `"}}`,
	}
	for i, p := range payloads {
		_, err := DecodeOperation([]byte(p))
		if err == nil {
			t.Fatalf("case %d: expected error", i)
		}
		if strings.Contains(err.Error(), sentinel) {
			t.Fatalf("case %d: error echoes param value: %v", i, err)
		}
	}
	// Oversized body whose tail is the sentinel.
	big := `{"operation":"o.p","target":{"document_id":"d"},"params":{"ok_key":"` + sentinel + `","pad":"` + strings.Repeat("p", MaxOperationBytes) + `"}}`
	_, err := DecodeOperation([]byte(big))
	if err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("oversized error must not echo values: %v", err)
	}
}
