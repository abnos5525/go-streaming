package h264

import "testing"

func TestWireHasBSlice(t *testing.T) {
	// NAL type 1, first_mb=0 (1 bit ue), slice_type=1 (B) → ue bits 010
	// header 0x01, rbsp: first_mb ue0 = 1, slice_type ue1 = 010 → 0b1_010xxxx = 0xa0
	b := []byte{0x01, 0xa0}
	if !WireHasBSlice(b) {
		t.Fatal("want B-slice")
	}
	// slice_type=0 (P): first_mb ue0=1, slice_type ue0=1 → 0b11xxxxxx = 0xc0
	p := []byte{0x01, 0xc0}
	if WireHasBSlice(p) {
		t.Fatal("P-slice must not match")
	}
	// IDR type 5 never B
	if WireHasBSlice([]byte{0x05, 0xa0}) {
		t.Fatal("IDR must not match")
	}
	// FU-A start of type-1 B
	fu := []byte{0x1c, 0x81, 0xa0} // FU-A, S=1, type=1, rbsp B
	if !WireHasBSlice(fu) {
		t.Fatal("want FU-A B-slice")
	}
	// STAP-A with one B NAL
	stap := []byte{0x18, 0x00, 0x02, 0x01, 0xa0}
	if !WireHasBSlice(stap) {
		t.Fatal("want STAP-A B-slice")
	}
}

func TestSizeFromSPS(t *testing.T) {
	// 1280x720 High SPS from mediacommon's suite
	nal := []byte{
		0x67, 0x64, 0x00, 0x1f, 0xac, 0xd9, 0x40, 0x50,
		0x05, 0xbb, 0x01, 0x6c, 0x80, 0x00, 0x00, 0x03,
		0x00, 0x80, 0x00, 0x00, 0x1e, 0x07, 0x8c, 0x18,
		0xcb,
	}
	w, h := SizeFromSPS(nal)
	if w != 1280 || h != 720 {
		t.Fatalf("sps size %dx%d", w, h)
	}
	if w, h := SizeFromSPS(nil); w != 0 || h != 0 {
		t.Fatalf("bad sps %dx%d", w, h)
	}
}
