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
| PTZ (WebSocket) | — | `GET /ptz?id={id}` |
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

With a built SPA, open **http://127.0.0.1:8085/docs/** on the same process as the API (no separate Vite port). For UI development only: `pnpm dev` → **http://127.0.0.1:5173/docs/**.

1. The map is Record → go-streaming → React. **کلمات کلیدی** opens the glossary and lights the matching station.
2. Click the **go-streaming** card. The inside view has 24 steps: file, method, and the media/control planes for that step.
3. **سناریوی پخش** calls the live API. Start the Go server first (`go run .` on port `8085`), then Start with a real `udp`/`rtsp` URL. The page shows the playback URL and a link to `/play/{id}`. Stop sends `POST /stream/stop` for the same source URL.
4. **↩ برگشت به نقشه** returns to the map.

```bash
cd web
pnpm build
```

Then restart `go run .` from the repo root. The server serves `web/dist` at `/docs/`. Override with `DOCS_ROOT=/path/to/dist` if needed. The lockfile is `web/pnpm-lock.yaml`. Do not use npm.

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
| `WS` | `/ptz?id={session_id}` | JSON `{"type":"ptz:move","x","y","zoom"}` → `ptz:ack` (stub RPC) |
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

`GET /auth/token` mints a one-hour HS256 JWT when `AUTH_SECRET` is set (same as `go run ./cmd/token`). The docs **scenario** uses **دریافت توکن** to fill a read-only field; **Start** is blocked until then. `GET /` without a valid `Authorization: Bearer` header returns `401`. Stream routes stay public.

On PowerShell, set the variable in the same window before both commands, or rely on `.env` (`AUTH_SECRET` is already set in the local `.env`). On bash, `export` is the same idea.

### gRPC control

`.env` sets `GRPC_ADDR=127.0.0.1:9095`. The process listens there with server reflection.

```bash
grpcurl -plaintext 127.0.0.1:9095 list
grpcurl -plaintext -d "{\"url\":\"udp://127.0.0.1:5600\",\"output_type\":6}" 127.0.0.1:9095 stream.v1.Control/Start
grpcurl -plaintext 127.0.0.1:9095 stream.v1.Control/Status
```

`output_type` is `6` (LL-HLS) or `7` (WebRTC). Stop uses the same source URL.

### WebSocket PTZ (stub)

After **Start**, open `ws://127.0.0.1:8085/ptz?id={session_id}` (same 16-hex id as HLS/WebRTC paths). Send:

```json
{"type":"ptz:move","x":0,"y":1,"zoom":0,"deviceId":"optional"}
```

The server replies with a JSON ack and logs `[ptz]` (no real camera RPC in this repo). The **scenario** on `/docs/` connects automatically; PTZ output appears in the terminal below the video.

For **تصویر محلی** (`udp://127.0.0.1:5600`), the server starts an internal GStreamer ball feed on Start — no manual `gst-launch` required when `GST_ROOT` is set.

### Demo camera

`GET /demo` and the scenario source field use a public lake webcam:

```text
rtsp://stream.strba.sk:1935/strba/VYHLAD_JAZERO.stream
```

Start plays that H.264 picture. The scenario button «تصویر محلی» switches to `udp://127.0.0.1:5600` and the server publishes the test ball automatically on Start.

RTSP ingest needs GStreamer. Set `GST_ROOT` in `.env` to the install root (for example `D:\gstreamer\PFiles64\gstreamer\1.0\msvc_x86_64`) or put `gst-launch-1.0` on PATH. `GST_ROOT` also sets plugin paths for child pipelines. The play page loads hls.js from `/vendor/hls.min.js` on the same server, and the docs scenario plays the picture in the page.

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
