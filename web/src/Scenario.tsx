import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { PtzJoystick } from "@/components/PtzJoystick";
import { StreamPanel } from "@/components/StreamPanel";
import { apiBaseURL } from "@/config/api";
import type { PtzAck } from "@/ptzSocket";

type StartBody = { url: string; output_type: number };
const demoCamera = "rtsp://stream.strba.sk:1935/strba/VYHLAD_JAZERO.stream";
const localCamera = "udp://127.0.0.1:5600"; // server starts ball feed on Start; GST_ROOT in .env

function streamId(mediaURL: string) {
  return mediaURL.match(/\/(?:hls|webrtc)\/([0-9a-f]{16})\//i)?.[1] ?? "";
}

function playURL(mediaURL: string) {
  const id = streamId(mediaURL);
  if (!id) return "";
  return `${new URL(mediaURL).origin}/play/${id}`;
}

function playbackURL(origin: string, id: string, lane: 6 | 7) {
  if (!id) return "";
  const base = origin.replace(/\/$/, "");
  return lane === 6 ? `${base}/hls/${id}/index.m3u8` : `${base}/webrtc/${id}/offer`;
}

async function apiError(res: Response) {
  const body = (await res.json().catch(() => null)) as { message?: string } | null;
  return body?.message || `${res.status}`;
}

export function Scenario() {
  const [source, setSource] = useState(demoCamera);
  const [outputType, setOutputType] = useState<6 | 7>(6);
  const [mediaURL, setMediaURL] = useState("");
  const [error, setError] = useState("");
  const [token, setToken] = useState("");
  const [tokenReady, setTokenReady] = useState(false);
  const [toast, setToast] = useState("");
  const [liveError, setLiveError] = useState("");
  const [streamLive, setStreamLive] = useState(false);
  const [busy, setBusy] = useState(false);
  const [ptzOut, setPtzOut] = useState<PtzAck | null>(null);
  const viewRef = useRef<HTMLDivElement>(null);

  const origin = apiBaseURL;
  const sessionId = streamId(mediaURL);
  const playMediaURL = sessionId ? playbackURL(origin, sessionId, outputType) : mediaURL;
  const playerURL = sessionId ? playURL(playMediaURL) : "";

  const onPtzOutput = useCallback((ack: PtzAck) => {
    setPtzOut(ack);
  }, []);

  const streamOutputs = useMemo(() => (ptzOut ? { ptz: ptzOut } : undefined), [ptzOut]);

  useEffect(() => {
    if (!sessionId) setPtzOut(null);
  }, [sessionId]);

  useEffect(() => {
    const ac = new AbortController();
    fetch(`${origin}/demo`, { signal: ac.signal })
      .then((res) => (res.ok ? res.json() : null))
      .then((data: { url?: string } | null) => {
        if (data?.url) setSource(data.url);
      })
      .catch(() => {});
    return () => ac.abort();
  }, [origin]);

  useEffect(() => {
    const id = streamId(mediaURL);
    if (!id) {
      setStreamLive(false);
      return;
    }
    const meta = () => {
      void fetch(`${origin}/stream/meta/${id}`)
        .then(async (res) => {
          if (!res.ok) return;
          const body = (await res.json()) as {
            error?: string;
            hls_ready?: boolean;
            webrtc_ready?: boolean;
          };
          setLiveError(body.error ?? "");
          setStreamLive(outputType === 7 ? body.webrtc_ready === true : body.hls_ready === true);
        })
        .catch(() => undefined);
    };
    meta();
    const timer = window.setInterval(meta, 2000);
    return () => window.clearInterval(timer);
  }, [mediaURL, origin, outputType]);

  useEffect(() => {
    if (!mediaURL) return;
    viewRef.current?.scrollIntoView({ block: "center" });
  }, [mediaURL]);

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(""), 4500);
    return () => window.clearTimeout(timer);
  }, [toast]);

  async function fetchToken() {
    setBusy(true);
    setError("");
    try {
      const res = await fetch(`${origin}/auth/token`);
      const body = (await res.json().catch(() => null)) as { token?: string; message?: string } | null;
      if (!res.ok || !body?.token) {
        throw new Error(body?.message || (await apiError(res)));
      }
      setToken(body.token);
      setTokenReady(true);
      const check = await fetch(`${origin}/`, { headers: { Authorization: `Bearer ${body.token}` } });
      if (!check.ok) throw new Error("توکن دریافت شد ولی GET / رد شد");
    } catch (err) {
      setToken("");
      setTokenReady(false);
      setToast(err instanceof Error ? err.message : "دریافت توکن ناموفق");
    } finally {
      setBusy(false);
    }
  }

  async function armStream(lane: 6 | 7) {
    if (!tokenReady || !token.trim()) {
      setToast("خطای دسترسی — ابتدا توکن را دریافت کنید");
      return;
    }
    setBusy(true);
    setError("");
    const body: StartBody = { url: source.trim(), output_type: lane };
    try {
      const res = await fetch(`${origin}/stream/start`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(await apiError(res));
      const data = (await res.json()) as { url?: string };
      if (!data.url) throw new Error("پاسخ بدون url");
      setOutputType(lane);
      setMediaURL(data.url);
    } catch (err) {
      if (!sessionId) setMediaURL("");
      setError(err instanceof Error ? err.message : "start failed");
    } finally {
      setBusy(false);
    }
  }

  async function start() {
    await armStream(outputType);
  }

  function onOutputTypeChange(next: 6 | 7) {
    if (next === outputType) return;
    if (!sessionId) {
      setOutputType(next);
      return;
    }
    void armStream(next);
  }

  async function stop() {
    setBusy(true);
    setError("");
    try {
      const res = await fetch(`${origin}/stream/stop`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: source.trim(), output_type: outputType }),
      });
      if (!res.ok) throw new Error(await apiError(res));
      setMediaURL("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "stop failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="scene on scenario" aria-label="سناریوی پخش">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void start();
        }}
      >
        <h2>سناریوی پخش</h2>
        <p className="world-hint">
          سرور Go باید روی پورت ۸۰۸۵ بالا باشد. Start همان قرارداد واقعی API را صدا می‌زند.
        </p>
        <label>
          آدرس منبع
          <input value={source} onChange={(event) => setSource(event.target.value)} spellCheck={false} />
        </label>
        <p className="world-hint">آدرس پیش‌فرض دوربین عمومی دریاچه است. Start همان تصویر زنده را پخش می‌کند. دکمهٔ تصویر محلی توپ آزمایشی را می‌گذارد.</p>
        <div className="row">
          <Button type="button" disabled={busy} onClick={() => setSource(demoCamera)}>
            دوربین دریاچه
          </Button>
          <Button type="button" disabled={busy} onClick={() => setSource(localCamera)}>
            تصویر محلی
          </Button>
        </div>
        <label>
          توکن JWT
          <input
            value={token}
            readOnly={tokenReady}
            spellCheck={false}
            placeholder="روی «دریافت توکن» بزنید"
            className={tokenReady ? "token-locked" : undefined}
          />
        </label>
        <div className="row">
          <Button type="button" disabled={busy || tokenReady} onClick={() => void fetchToken()}>
            دریافت توکن
          </Button>
        </div>
        <label>
          نوع خروجی
          <select
            value={outputType}
            disabled={busy}
            onChange={(event) => onOutputTypeChange(event.target.value === "7" ? 7 : 6)}
          >
            <option value={6}>LL-HLS</option>
            <option value={7}>WebRTC</option>
          </select>
        </label>
        <div className="row">
          <Button type="submit" variant="primary" className="btn primary" disabled={busy}>
            Start
          </Button>
          <Button type="button" disabled={busy} onClick={() => void stop()}>
            Stop
          </Button>
        </div>
        {error ? <p className="err">{error}</p> : null}
        {mediaURL ? (
          <div className="scenario-stage box" ref={viewRef}>
            <StreamPanel
              origin={origin}
              mediaURL={playMediaURL || mediaURL}
              outputType={outputType}
              playerURL={playerURL}
              liveError={liveError}
              outputs={streamOutputs}
              viewKey={`${sessionId}-${outputType}`}
            />
            <PtzJoystick
              apiOrigin={origin}
              streamId={sessionId}
              disabled={!sessionId}
              streamLive={streamLive}
              onOutput={onPtzOutput}
            />
          </div>
        ) : null}
      </form>
      {toast ? (
        <div className="scenario-toast" role="alert">
          {toast}
        </div>
      ) : null}
    </section>
  );
}
