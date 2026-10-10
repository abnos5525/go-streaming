package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPlayerPagesAreSingleLane(t *testing.T) {
	id := "e903d3fad593f281"
	hls := watchPage(id, 6)
	if strings.Count(hls, "<video") != 1 || strings.Contains(hls, "startRTC") || strings.Contains(hls, "v2") {
		t.Fatal("output_type 6 must be LL-HLS only")
	}
	if !strings.Contains(hls, "lowLatencyMode: true") || strings.Contains(hls, "liveSyncDurationCount") {
		t.Fatal("LL-HLS player must use PART-HOLD-BACK, not a 2-segment sync")
	}
	if !strings.Contains(hls, "enableWorker: false") {
		t.Fatal("Firefox MSE + hls.js worker stalls LL parts")
	}
	if !strings.Contains(hls, "recoverMediaError") || !strings.Contains(hls, "startLoad") {
		t.Fatal("LL-HLS must recover Chrome MSE stalls without a full refresh")
	}
	if strings.Contains(hls, "maxBufferLength") || strings.Contains(hls, "maxLiveSyncPlaybackRate") {
		t.Fatal("LL-HLS must not shrink the MSE buffer or chase live edge")
	}
	rtc := watchPage(id, 7)
	if strings.Count(rtc, "<video") != 1 || strings.Contains(rtc, "hls.js") || strings.Contains(rtc, "index.m3u8") {
		t.Fatal("output_type 7 must be WebRTC only")
	}
	if !strings.Contains(rtc, "iceServers:[]") || !strings.Contains(rtc, "setTimeout(r, 800)") {
		t.Fatal("WebRTC page must not stall on blocked STUN gathering")
	}
	if !strings.Contains(rtc, "new MediaStream([e.track])") {
		t.Fatal("Firefox ontrack often has empty streams")
	}
	if !strings.Contains(rtc, "setCodecPreferences") || !strings.Contains(rtc, "profile-level-id=(4d|64)") {
		t.Fatal("prefer Main/High H264 over Baseline so Main cameras are not black")
	}
	if !strings.Contains(rtc, "no video") {
		t.Fatal("ICE-connected black must re-offer")
	}
	if !strings.Contains(rtc, "frozen") {
		t.Fatal("Chrome stuck currentTime must re-offer")
	}
	if !strings.Contains(rtc, "jitterBufferTarget = 200") || !strings.Contains(rtc, "playoutDelayHint = 0.2") {
		t.Fatal("WebRTC must cap Chrome jitter buffer without a 50ms underrun")
	}
	if !strings.Contains(trackerJS, "ovCur.width") {
		t.Fatal("overlay must scale from payload size, not wait for a painted WebRTC frame")
	}
	if !strings.Contains(trackerJS, "holdFrames") || !strings.Contains(trackerJS, "requestVideoFrameCallback") {
		t.Fatal("overlay must Hold on the painted video frame, not a sticky poll pick")
	}
	if !strings.Contains(hls, "/stream/overlay/' + ovID") || !strings.Contains(hls, "setInterval(pull, 100)") {
		t.Fatal("LL-HLS overlay stays a throttled HTTP GET")
	}
	if !strings.Contains(rtc, "/viewers") || !strings.Contains(rtc, "createDataChannel(slot.channel") || !strings.Contains(rtc, "slot.offer") {
		t.Fatal("each WebRTC viewer must POST its own address and data channel")
	}
	if strings.Contains(rtc, "playerLagMS(v)") {
		t.Fatal("WebRTC must not HTTP-poll overlay")
	}
	if strings.Contains(trackerJS, "EventSource") || strings.Contains(trackerJS, "WebSocket") || strings.Contains(trackerJS, "/stream/overlay/ws/") {
		t.Fatal("overlay must not use EventSource or WebSocket behind a reverse proxy")
	}
	if !strings.Contains(trackerJS, "if (c.width !== w)") {
		t.Fatal("overlay must not reset canvas size every animation frame")
	}
	if strings.Contains(rtc, "connectionState === 'disconnected'") {
		t.Fatal("brief ICE disconnected must not tear down WebRTC")
	}
	if !strings.Contains(playerCSS, "overflow:hidden") || !strings.Contains(playerCSS, "max-height:100vh") {
		t.Fatal("player must fit the viewport without scroll")
	}
}

func TestAbsURLBehindProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/stream/start", nil)
	c.Request.Host = "127.0.0.1:8085"
	if got := absURL(c, "/webrtc/abcdabcdabcdabcd/offer"); got != "http://127.0.0.1:8085/webrtc/abcdabcdabcdabcd/offer" {
		t.Fatalf("direct: %s", got)
	}
	c.Request.Header.Set("X-Forwarded-Proto", "https, http")
	c.Request.Header.Set("X-Forwarded-Host", "stream.example.com")
	if got := absURL(c, "/webrtc/abcdabcdabcdabcd/offer"); got != "https://stream.example.com/webrtc/abcdabcdabcdabcd/offer" {
		t.Fatalf("proxy: %s", got)
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
