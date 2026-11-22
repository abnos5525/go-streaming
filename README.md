# go-streaming

Low-latency live video server in Go. Ingest camera multicast (UDP/RTP) or RTSP, then play in the browser as **Apple LL-HLS** or **WebRTC**. Optional AI bounding boxes are drawn on a canvas overlay (not burned into the video).

No lock on stream routes. `GET /` checks an HS256 JWT only when `AUTH_SECRET` is set.

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

### Docs site

The home page is a React walkthrough. Install [pnpm](https://pnpm.io/installation) once, then:

```bash
cd web
pnpm install
pnpm dev
```

Open **http://127.0.0.1:5173**.

1. The map is Record → go-streaming → React. **کلمات کلیدی** opens the glossary and lights the matching station.
2. Click the **go-streaming** card. The inside view has 24 steps: file, method, and the media/control planes for that step.
3. **سناریوی پخش** calls the live API. Start the Go server first (`go run .` on port `8085`), then Start with a real `udp`/`rtsp` URL. The page shows the playback URL and a link to `/play/{id}`. Stop sends `POST /stream/stop` for the same source URL.
4. **↩ برگشت به نقشه** returns to the map.

`pnpm build` typechecks and writes `web/dist`. The lockfile is `web/pnpm-lock.yaml`. Do not use npm.

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

### JWT on `GET /`

Leave `AUTH_SECRET` unset and `GET /` stays open (`{"message":"ok"}`).

To turn the check on:

```bash
set AUTH_SECRET=dev-secret
go run ./cmd/token
go run .
```

`cmd/token` prints a token valid for one hour. In the docs scenario page, paste it and press **GET /**. Without the header the same call returns `401`. `POST /stream/start` does not use this token.

On PowerShell, set the variable in the same window before both commands, or rely on `.env` (`AUTH_SECRET` is already set in the local `.env`). On bash, `export` is the same idea.

### gRPC control

`.env` sets `GRPC_ADDR=127.0.0.1:9095`. The process listens there with server reflection.

```bash
grpcurl -plaintext 127.0.0.1:9095 list
grpcurl -plaintext -d "{\"url\":\"udp://127.0.0.1:5600\",\"output_type\":6}" 127.0.0.1:9095 stream.v1.Control/Start
grpcurl -plaintext 127.0.0.1:9095 stream.v1.Control/Status
```

`output_type` is `6` (LL-HLS) or `7` (WebRTC). Stop uses the same source URL.

### Demo camera

`GET /demo` and the scenario source field use the official Wowza test camera:

```text
rtsp://9627b0bf2a7b.entrypoint.cloud.wowza.com:1935/app-p5260J38/66abe4b9_stream1
```

From this network that host answers RTSP SETUP with 403, so Start does not show a picture. The scenario button «تصویر محلی» switches the field to `udp://127.0.0.1:5600`. Publish that picture with GStreamer before pressing Start:

```powershell
gst-launch-1.0 videotestsrc is-live=true pattern=ball ! video/x-raw,width=640,height=360,framerate=25/1 ! x264enc tune=zerolatency bitrate=800 speed-preset=ultrafast key-int-max=25 ! rtph264pay pt=96 config-interval=1 ! udpsink host=127.0.0.1 port=5600
```

RTSP ingest needs `gst-launch-1.0` on PATH. This machine uses the extracted runtime at `D:\gstreamer\PFiles64\gstreamer\1.0\msvc_x86_64`. The user PATH and `GST_PLUGIN_PATH` point there. The play page loads hls.js from `/vendor/hls.min.js` on the same server, and the docs scenario plays the picture in the page.

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
