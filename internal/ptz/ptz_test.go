package ptz

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/websocket"
)

func TestDirection(t *testing.T) {
	cases := []struct {
		x, y, z float64
		want    string
	}{
		{0, 0, 0, "stop"},
		{0, 0, 1, "zoom_in"},
		{1, 0, 0, "right"},
		{0, 1, 0, "up"},
	}
	for _, c := range cases {
		if got := Direction(c.x, c.y, c.z); got != c.want {
			t.Fatalf("Direction(%v,%v,%v)=%q want %q", c.x, c.y, c.z, got, c.want)
		}
	}
}

func TestWSMoveAck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(ServeHTTP))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, err := websocket.Dial(url, "", "http://127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := websocket.Message.Send(conn, `{"type":"ptz:move","x":1,"y":0,"deviceId":"cam1"}`); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := websocket.Message.Receive(conn, &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"ptz:ack"`) || !strings.Contains(got, `"right"`) || !strings.Contains(got, `"pan":1`) {
		t.Fatal(got)
	}
}