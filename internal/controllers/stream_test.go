package controllers

import "testing"

func TestOutPath(t *testing.T) {
	id := "e903d3fad593f281"
	hls, err := outPath(id, 6)
	if err != nil || hls != "/hls/"+id+"/index.m3u8" {
		t.Fatalf("hls: %q %v", hls, err)
	}
	rtc, err := outPath(id, 7)
	if err != nil || rtc != "/webrtc/"+id+"/offer" {
		t.Fatalf("rtc: %q %v", rtc, err)
	}
	if _, err := outPath(id, 5); err == nil {
		t.Fatal("want reject")
	}
}
