package controllers

import (
	"strings"
	"testing"
)

func TestPlayerPagesAreSingleLane(t *testing.T) {
	id := "e903d3fad593f281"
	hls := watchPage(id, 6)
	if strings.Count(hls, "<video") != 1 || strings.Contains(hls, "startRTC") || strings.Contains(hls, "v2") {
		t.Fatal("output_type 6 must be HLS only")
	}
	rtc := watchPage(id, 7)
	if strings.Count(rtc, "<video") != 1 || strings.Contains(rtc, "hls.js") || strings.Contains(rtc, "index.m3u8") {
		t.Fatal("output_type 7 must be WebRTC only")
	}
}

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
