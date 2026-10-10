package h264

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestParseSEI(t *testing.T) {
	js := []byte(`{"camera":{"id":"x"},"frame":{"timestamp":1}}`)
	p := append(bytes.Repeat([]byte{0xab}, 16), js...)
	nal := []byte{0x06, 5, byte(len(p))}
	nal = append(nal, p...)
	nal = append(nal, 0x80)

	if ParseSEI(nil) != nil || ParseSEI([]byte{0x65}) != nil {
		t.Fatal("non-sei")
	}
	msgs := ParseSEI(nal)
	if len(msgs) != 1 || msgs[0].Type != 5 || msgs[0].UUID == "" || msgs[0].Data == nil {
		t.Fatalf("%+v", msgs)
	}
	var got map[string]any
	if err := json.Unmarshal(msgs[0].Data, &got); err != nil {
		t.Fatal(err)
	}
	cam := got["camera"].(map[string]any)
	if cam["id"] != "x" {
		t.Fatalf("payload: %v", got)
	}
}
