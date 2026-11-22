import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { LiveView } from "@/LiveView";

type StartBody = { url: string; output_type: number };

const defaultAPI = "http://127.0.0.1:8085";
const demoCamera = "udp://127.0.0.1:5600";
const wowzaCamera = "rtsp://9627b0bf2a7b.entrypoint.cloud.wowza.com:1935/app-p5260J38/66abe4b9_stream1";

function streamId(mediaURL: string) {
  return mediaURL.match(/\/(?:hls|webrtc)\/([0-9a-f]{16})\//i)?.[1] ?? "";
}

function playURL(mediaURL: string) {
  const id = streamId(mediaURL);
  if (!id) return "";
  return `${new URL(mediaURL).origin}/play/${id}`;
}

async function apiError(res: Response) {
  const body = (await res.json().catch(() => null)) as { message?: string } | null;
  return body?.message || `${res.status}`;
}

export function Scenario() {
  const [api, setApi] = useState(defaultAPI);
  const [source, setSource] = useState(demoCamera);
  const [outputType, setOutputType] = useState<6 | 7>(6);
  const [mediaURL, setMediaURL] = useState("");
  const [error, setError] = useState("");
  const [token, setToken] = useState("");
  const [gate, setGate] = useState("");
  const [liveError, setLiveError] = useState("");
  const [busy, setBusy] = useState(false);

  const origin = api.replace(/\/$/, "");
  const player = mediaURL ? playURL(mediaURL) : "";

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
    if (!id) return;
    const timer = window.setInterval(() => {
      void fetch(`${origin}/stream/meta/${id}`)
        .then(async (res) => {
          if (!res.ok) return;
          const meta = (await res.json()) as { error?: string };
          setLiveError(meta.error ?? "");
        })
        .catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [mediaURL, origin]);

  async function start() {
    setBusy(true);
    setError("");
    const body: StartBody = { url: source.trim(), output_type: outputType };
    try {
      const res = await fetch(`${origin}/stream/start`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(await apiError(res));
      const data = (await res.json()) as { url?: string };
      if (!data.url) throw new Error("پاسخ بدون url");
      setMediaURL(data.url);
    } catch (err) {
      setMediaURL("");
      setError(err instanceof Error ? err.message : "start failed");
    } finally {
      setBusy(false);
    }
  }

  async function stop() {
    setBusy(true);
    setError("");
    try {
      const res = await fetch(`${origin}/stream/stop`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: source.trim() }),
      });
      if (!res.ok) throw new Error(await apiError(res));
      setMediaURL("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "stop failed");
    } finally {
      setBusy(false);
    }
  }

  async function whoami() {
    setBusy(true);
    setGate("");
    const headers = new Headers();
    if (token.trim()) headers.set("Authorization", `Bearer ${token.trim()}`);
    try {
      const res = await fetch(`${origin}/`, { headers });
      const body = (await res.json().catch(() => null)) as { message?: string } | null;
      setGate(`${res.status} ${body?.message ?? ""}`.trim());
    } catch (err) {
      setGate(err instanceof Error ? err.message : "GET / failed");
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
          آدرس API
          <input value={api} onChange={(event) => setApi(event.target.value)} spellCheck={false} />
        </label>
        <label>
          آدرس منبع
          <input value={source} onChange={(event) => setSource(event.target.value)} spellCheck={false} />
        </label>
        <p className="world-hint">آدرس پیش‌فرض تصویر محلی است و Start همین را پخش می‌کند. دوربین Wowza از این شبکه ۴۰۳ می‌دهد و تصویر ندارد.</p>
        <div className="row">
          <Button type="button" disabled={busy} onClick={() => setSource(demoCamera)}>
            تصویر محلی
          </Button>
          <Button type="button" disabled={busy} onClick={() => setSource(wowzaCamera)}>
            دوربین Wowza
          </Button>
        </div>
        <label>
          نوع خروجی
          <select
            value={outputType}
            onChange={(event) => setOutputType(event.target.value === "7" ? 7 : 6)}
          >
            <option value={6}>۶ · LL-HLS</option>
            <option value={7}>۷ · WebRTC</option>
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
          <div className="box">
            <h4>پاسخ سرور</h4>
            <p className="prose">
              <a href={mediaURL}>{mediaURL}</a>
            </p>
            {player ? <LiveView origin={origin} mediaURL={mediaURL} outputType={outputType} /> : null}
            {liveError ? <p className="err">{liveError}</p> : null}
          </div>
        ) : null}
        <label>
          توکن JWT
          <input
            value={token}
            onChange={(event) => setToken(event.target.value)}
            spellCheck={false}
            placeholder="خالی = درخواست بدون Authorization"
          />
        </label>
        <div className="row">
          <Button type="button" disabled={busy} onClick={() => void whoami()}>
            GET /
          </Button>
        </div>
        {gate ? <p className="prose">{gate}</p> : null}
      </form>
    </section>
  );
}
