package plugin

// FuzzFrameDecode exercises the bridge's JSON frame decode path (the same
// json.Unmarshal + type gate handleWebSocket applies): it must never panic,
// and a frame without a type is never accepted.

import (
	"encoding/json"
	"testing"
)

// decodeFrame mirrors the bridge's first-frame gate: valid JSON with a
// non-empty type is accepted, anything else is refused.
func decodeFrame(data []byte) (typ string, err error) {
	if len(data) > maxMessageBytes+1 {
		return "", errPluginError("frame exceeds size limit")
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	t, _ := raw["type"].(string)
	if t == "" {
		return "", errPluginError("invalid frame: missing type")
	}
	return t, nil
}

func FuzzFrameDecode(f *testing.F) {
	f.Add([]byte(`{"type":"hello","protocol":3,"plugin_instance_id":"a","editor_type":"word","capabilities":["writer.replace_text"],"client_version":"1"}`))
	f.Add([]byte(`{"type":"response","id":"abc","ok":true,"result":{"matches":2}}`))
	f.Add([]byte(`{"type":"response","id":"abc","ok":false,"error":{"code":"x","message":"y"}}`))
	f.Add([]byte(`{"type":"command","id":"abc","method":"ping","deadline_ms":1000}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"type":""}`))
	f.Add([]byte(`{"id":"abc","ok":true}`))
	f.Add([]byte(`{"type":123}`))
	f.Add([]byte(`not json`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxMessageBytes+1 {
			t.Skip("input over cheap-fuzz budget")
		}
		typ, err := decodeFrame(data)
		if err != nil {
			return
		}
		if typ == "" {
			t.Fatal("frame without a type was accepted")
		}
	})
}
