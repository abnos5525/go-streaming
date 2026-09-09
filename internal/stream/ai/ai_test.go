package ai

import "testing"

func TestParseAIFrame(t *testing.T) {
	raw := []byte(`{"camera":{"id":"x"},"frame":{"timestamp":1},"results":[{"model":"FaceAnalysis","objects":[{"bbox":{"x":50,"y":50,"width":100,"height":100},"confidence":0.8,"object_type_name":"face"}]}],"type_message":6}`)
	f := Parse(raw)
	if f == nil || f.Results[0].Model != "FaceAnalysis" || f.Results[0].Objects[0].Name != "face" {
		t.Fatal(f)
	}
	naked := []byte(`"camera":{"id":"x"},"frame":{"timestamp":1},"results":[{"model":"FaceAnalysis","objects":[]}],"type_message":6`)
	if Parse(naked) == nil || Parse(naked).Results[0].Model != "FaceAnalysis" {
		t.Fatal("wrap")
	}
	if Parse([]byte{0x80, 96, 1, 2, 3}) != nil {
		t.Fatal("rtp junk")
	}
	if _, err := ParseErr(nil); err == nil {
		t.Fatal("empty")
	}
	// Live heartbeat: nothing detected, but a valid envelope with camera id.
	hb := []byte(`{"camera":{"id":"cam1"},"frame":{"timestamp":1788936587571},"results":[]}`)
	if f := Parse(hb); f == nil || f.Camera.ID != "cam1" || len(f.Results) != 0 {
		t.Fatalf("heartbeat rejected: %v", f)
	}
	if _, err := ParseErr(hb); err != nil {
		t.Fatalf("heartbeat err: %v", err)
	}
	// Envelope without camera id is still rejected (no sync anchor).
	if _, err := ParseErr([]byte(`{"results":[]}`)); err == nil {
		t.Fatal("want error for camera-less frame")
	}
}

func TestSEIFromNAL(t *testing.T) {
	js := []byte(`{"camera":{"id":"x"},"frame":{"timestamp":1},"results":[{"model":"FaceAnalysis","objects":[]}],"type_message":6}`)
	p := append(make([]byte, 16), js...)
	sei := []byte{0x06, 5, byte(len(p))}
	sei = append(sei, p...)
	sei = append(sei, 0x80)
	if FromNAL(nil) != nil || FromNAL([]byte{1}) != nil {
		t.Fatal("non-sei")
	}
	f := FromNAL(sei)
	if f == nil || f.Results[0].Model != "FaceAnalysis" || f.Frame.Timestamp != 1 {
		t.Fatal(f)
	}
}

func TestJSONSlice(t *testing.T) {
	js := `{"camera":{"id":"camX"},"frame":{"timestamp":7},"results":[]}`
	if j := jsonSlice(append([]byte{0xAA, 0xBB}, append([]byte(js), 0xCC)...)); string(j) != js {
		t.Fatalf("jsonSlice: %q", j)
	}
	if jsonSlice([]byte{0x01, 0x02}) != nil || jsonSlice([]byte(`{"a":`)) != nil {
		t.Fatal("jsonSlice false positive")
	}
	// Strings containing braces must not break balance.
	tricky := `{"a":"} not the end","b":[1,2]}`
	if j := jsonSlice([]byte("xx" + tricky + "yy")); string(j) != tricky {
		t.Fatalf("jsonSlice strings: %q", j)
	}
}
