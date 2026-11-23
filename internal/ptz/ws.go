package ptz

import (
	"net/http"

	"golang.org/x/net/websocket"
)

func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			defer ws.Close()
			id := r.URL.Query().Get("id")
			for {
				var raw string
				if err := websocket.Message.Receive(ws, &raw); err != nil {
					return
				}
				ack := Handle(id, []byte(raw))
				if len(ack) == 0 {
					continue
				}
				if err := websocket.Message.Send(ws, string(ack)); err != nil {
					return
				}
			}
		},
	}.ServeHTTP(w, r)
}