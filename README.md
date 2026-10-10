# go-streaming

Low-latency live video server in Go. Ingest camera multicast (UDP/RTP) or RTSP, then play in the browser as **Apple LL-HLS** or **WebRTC**. Optional AI bounding boxes are drawn on a canvas overlay (not burned into the video).

No authentication. All stream routes are public.

| | |
|---|---|
| Port | `8085` |
| Go | 1.27+ |
| Module | `github.com/abnos5525/go-streaming` |

---

## What you get

| Output | Type | Path |
|---|---|---|
| Apple LL-HLS (gohlslib) | `6` | `/hls/{id}/index.m3u8` |
| WebRTC (pion) | `7` | `/webrtc/{id}/offer` |
| Built-in player | — | `/play/{id}` |
| Overlay JSON | — | `/stream/overlay/{id}` |
| OpenAPI UI | — | `/api/doc` |

Starting a stream with type `6` or `7` arms **both** lanes on the same ingest. `/play/{id}` shows the **latest** type you requested.

---

## Requirements

- **Go 1.27+** (`go version`)
- Network access to your camera multicast or RTSP URL
- For RTSP fallback remux only: **GStreamer** (`gst-launch-1.0`, plugins base/good/bad, libav). Multicast LL-HLS/WebRTC do **not** need GStreamer.
- Browser with [hls.js](https://github.com/video-dev/hls.js) support (Chrome/Firefox/Edge) or Safari native HLS

Optional: Docker (see below).

---

## Quick start (local)

```bash
git clone https://github.com/abnos5525/go-streaming.git
cd go-streaming
go mod download
go run .
```

Server listens on **http://127.0.0.1:8085**.

### 1. Start a stream

Default multicast example (change to your camera URL):

```bash
curl -s -X POST http://127.0.0.1:8085/stream/start \
  -H "Content-Type: application/json" \
  -d "{\"url\":\"udp://224.2.0.1:5484\",\"output_type\":6}"
```

Example response:

```json
{
  "url": "/hls/e903d3fad593f281/index.m3u8",
  "output_type": 6
}
```

The `id` is the first 16 hex chars of SHA-256 of the normalized source URL.

WebRTC:

```bash
curl -s -X POST http://127.0.0.1:8085/stream/start \
  -H "Content-Type: application/json" \
  -d "{\"url\":\"udp://224.2.0.1:5484\",\"output_type\":7}"
```

### 2. Watch

Open in a browser:

```
http://127.0.0.1:8085/play/<id>
```

Or point hls.js / VLC at the returned HLS URL, or POST an SDP offer to the WebRTC path (see `/api/doc`).

### 3. Stop

```bash
curl -s -X POST http://127.0.0.1:8085/stream/stop \
  -H "Content-Type: application/json" \
  -d "{\"url\":\"udp://224.2.0.1:5484\"}"
```

---

## API cheat sheet

| Method | Path | Body / notes |
|---|---|---|
| `GET` | `/` | `{"message":"ok"}` |
| `POST` | `/stream/start` | `{ "url", "output_type": 6\|7 }` |
| `POST` | `/stream/stop` | `{ "url" }` |
| `GET` | `/stream/meta` | All sessions |
| `GET` | `/stream/meta/{id}` | One session |
| `GET` | `/stream/overlay/{id}?lag_ms=0` | Detection boxes (JSON) |
| `GET` | `/hls/{id}/index.m3u8` | LL-HLS playlist |
| `POST` | `/webrtc/{id}/viewers` | Allocate viewer slot |
| `POST` | `/webrtc/{id}/v/{viewer}/offer` | SDP offer → answer |
| `GET` | `/play/{id}` | Built-in HTML player |
| `GET` | `/api/doc` | Swagger UI |

Allowed URL schemes: `udp`, `rtp`, `rtsp`, `rtsps`.

Full schemas: **http://127.0.0.1:8085/api/doc**

---

## Configuration (optional)

Copy `.env.example` → `.env` only if you need overrides. The process runs with **no env file**.

| Variable | Purpose |
|---|---|
| `HOST_IP` | Force this IPv4 for WebRTC host candidates and multicast NIC |
| `HLS_TIME` | LL-HLS target segment duration seconds (`0.25`–`6`) |
| `HLS_LIST_SIZE` | Playlist window (`7`–`15`) |
| `AI_OFFSET_MS` | Overlay timestamp offset |

---

## Docker

```bash
docker build -t go-streaming .
docker run --rm -p 8085:8085 \
  --network host \
  -e HOST_IP=192.168.1.10 \
  go-streaming
```

`--network host` (Linux) plus `HOST_IP` is the usual way to receive multicast and advertise a reachable WebRTC candidate. On Windows/macOS Docker, host networking differs — prefer running the binary on the host for multicast.

Image uses public `golang` / `debian` bases and installs GStreamer for RTSP remux.

---

## Build & test

```bash
go build -o go-streaming .
go test ./...
```

After a local run, stop the process so port `8085` is free.

---

## Architecture (short)

```
Camera multicast / RTSP
        │
        ▼
   sniff (UDP) ──► fanout
        │              ├─► LL-HLS muxer (gohlslib) ──► /hls/{id}/…
        │              └─► WebRTC hub (pion)      ──► /webrtc/{id}/…
        │
        └─► AI JSON → RAM → /stream/overlay/{id} → canvas on /play
```

- Overlay is **not** inside the HLS fragments or WebRTC pixels.
- Do not introduce ffmpeg; remux leftover uses `gst-launch-1.0` only when needed.

---

## License / status

Personal open sandbox. Use at your own risk on private networks. Lock down or put a reverse proxy in front if you expose it beyond LAN.
