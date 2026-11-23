import { useEffect, useRef } from "react";
import Hls from "hls.js";

type Slot = { offer: string; channel: string; dc_id: number };

export function LiveView({
  origin,
  mediaURL,
  outputType,
  active = true,
}: {
  origin: string;
  mediaURL: string;
  outputType: 6 | 7;
  active?: boolean;
}) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const id = mediaURL.match(/\/(?:hls|webrtc)\/([0-9a-f]{16})\//i)?.[1] ?? "";

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !id) return;
    const arm = () => {
      video.muted = true;
      void video.play().catch(() => undefined);
    };
    arm();
    if (outputType === 6) {
      if (Hls.isSupported()) {
        const player = new Hls({ lowLatencyMode: true, enableWorker: false });
        player.loadSource(mediaURL);
        player.attachMedia(video);
        player.on(Hls.Events.MANIFEST_PARSED, arm);
        player.on(Hls.Events.ERROR, (_, data) => {
          if (!data.fatal) return;
          if (data.type === Hls.ErrorTypes.MEDIA_ERROR) player.recoverMediaError();
          else if (data.type === Hls.ErrorTypes.NETWORK_ERROR) player.startLoad();
          arm();
        });
        let lastT = -1;
        let stuck = 0;
        const timer = window.setInterval(() => {
          if (video.paused || video.readyState < 2) {
            stuck = 0;
            lastT = video.currentTime;
            arm();
            return;
          }
          if (video.currentTime === lastT) {
            if (++stuck >= 6) {
              stuck = 0;
              player.startLoad();
            }
          } else {
            stuck = 0;
          }
          lastT = video.currentTime;
        }, 500);
        return () => {
          window.clearInterval(timer);
          player.destroy();
        };
      }
      video.src = mediaURL;
      arm();
      return;
    }

    let closed = false;
    let pc: RTCPeerConnection | null = null;
    const sleep = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms));
    void (async () => {
      while (!closed) {
        if (!(await webrtcReady(origin, id, () => closed))) return;
        pc?.close();
        const viewers = await fetch(`${origin}/webrtc/${id}/viewers`, { method: "POST" });
        if (closed) return;
        if (!viewers.ok) {
          await sleep(400);
          continue;
        }
        const slot = (await viewers.json()) as Slot;
        pc = new RTCPeerConnection({ iceServers: loopbackIce() });
        pc.addTransceiver("video", { direction: "recvonly" });
        if (firefoxUA()) preferBaselineH264(pc);
        else preferMainH264(pc);
        pc.createDataChannel(slot.channel, { negotiated: true, id: slot.dc_id });
        pc.ontrack = (event) => {
          const receiver = event.receiver as RTCRtpReceiver & { playoutDelayHint?: number };
          const ff = firefoxUA();
          const jt = ff ? 120 : 200;
          const pd = ff ? 0.12 : 0.2;
          try {
            receiver.jitterBufferTarget = jt;
            receiver.playoutDelayHint = pd;
          } catch {
            void receiver;
          }
          video.srcObject = event.streams[0] ?? new MediaStream([event.track]);
          arm();
        };
        try {
          const raw = await pc.createOffer();
          const offered = { type: raw.type, sdp: forcePacketMode1(raw.sdp ?? "") };
          try {
            await pc.setLocalDescription(offered);
          } catch {
            await pc.setLocalDescription(raw);
          }
          await waitForICE(pc);
          if (closed || !pc.localDescription) return;
          const offerURL = slot.offer.startsWith("http") ? slot.offer : `${origin}${slot.offer}`;
          const sdp = scrubOfferSdp(pc.localDescription.sdp);
          const answer = await fetch(offerURL, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ sdp, type: pc.localDescription.type }),
          });
          if (!answer.ok || closed) {
            await sleep(400);
            continue;
          }
          await pc.setRemoteDescription(await answer.json());
          arm();
          while (!closed && pc.connectionState !== "failed" && pc.iceConnectionState !== "failed") {
            if (await videoShowsFrame(video)) return;
          }
        } catch {
          await sleep(400);
        }
      }
    })();
    return () => {
      closed = true;
      pc?.close();
    };
  }, [id, mediaURL, origin, outputType]);

  useEffect(() => {
    if (!active) return;
    const video = videoRef.current;
    if (!video) return;
    video.muted = true;
    void video.play().catch(() => undefined);
  }, [active]);

  return <video ref={videoRef} className="live" autoPlay playsInline muted controls />;
}

function preferBaselineH264(pc: RTCPeerConnection) {
  const transceiver = pc.getTransceivers()[0];
  const caps = RTCRtpReceiver.getCapabilities("video");
  if (!transceiver?.setCodecPreferences || !caps) return;
  const h264 = caps.codecs.filter(
    (codec) => /H264/i.test(codec.mimeType) && /packetization-mode=1/.test(codec.sdpFmtpLine || ""),
  );
  const baseline = h264.filter((codec) => /profile-level-id=42/i.test(codec.sdpFmtpLine || ""));
  const pref = baseline.length > 0 ? baseline : h264;
  if (pref.length === 0) return;
  try {
    transceiver.setCodecPreferences(pref);
  } catch {
    return;
  }
}

function preferMainH264(pc: RTCPeerConnection) {
  const transceiver = pc.getTransceivers()[0];
  const caps = RTCRtpReceiver.getCapabilities("video");
  if (!transceiver?.setCodecPreferences || !caps) return;
  const h264 = caps.codecs.filter(
    (codec) => /H264/i.test(codec.mimeType) && /packetization-mode=1/.test(codec.sdpFmtpLine || ""),
  );
  const pref: RTCRtpCodec[] = [];
  const add = (codecs: RTCRtpCodec[]) => {
    for (const codec of codecs) if (!pref.includes(codec)) pref.push(codec);
  };
  add(h264.filter((codec) => /profile-level-id=(4d|64)/i.test(codec.sdpFmtpLine || "")));
  add(h264.filter((codec) => !/42e01f/.test(codec.sdpFmtpLine || "")));
  add(h264);
  add(caps.codecs);
  try {
    transceiver.setCodecPreferences(pref);
  } catch {
    return;
  }
}

function firefoxUA() {
  return /Firefox\//i.test(navigator.userAgent);
}

function isLoopbackOrigin() {
  const host = new URL(window.location.href).hostname;
  return host === "127.0.0.1" || host === "localhost" || host === "[::1]";
}

function loopbackIce(): RTCIceServer[] {
  return [];
}

function waitForICE(pc: RTCPeerConnection) {
  const cap = isLoopbackOrigin() ? 400 : 800;
  return Promise.race([
    new Promise<void>((resolve) => {
      if (pc.iceGatheringState === "complete") return resolve();
      const done = () => {
        if (pc.iceGatheringState === "complete") {
          pc.removeEventListener("icegatheringstatechange", done);
          resolve();
        }
      };
      pc.addEventListener("icegatheringstatechange", done);
    }),
    new Promise<void>((resolve) => {
      const hit = (event: Event) => {
        const candidate = (event as RTCPeerConnectionIceEvent).candidate?.candidate ?? "";
        const host = candidate.includes("typ host");
        const usable = host && (firefoxUA() || !candidate.includes(".local"));
        if (usable) {
          pc.removeEventListener("icecandidate", hit);
          resolve();
        }
      };
      pc.addEventListener("icecandidate", hit);
    }),
    new Promise<void>((resolve) => window.setTimeout(resolve, cap)),
  ]);
}

async function webrtcReady(origin: string, id: string, closed: () => boolean) {
  while (!closed()) {
    try {
      const res = await fetch(`${origin}/stream/meta/${id}`);
      if (res.ok) {
        const body = (await res.json()) as { webrtc_ready?: boolean };
        if (body.webrtc_ready) return true;
      }
    } catch {
      /* meta while the lane is still arming */
    }
    await new Promise((resolve) => window.setTimeout(resolve, 250));
  }
  return false;
}

function videoShowsFrame(video: HTMLVideoElement) {
  return new Promise<boolean>((resolve) => {
    const timer = window.setTimeout(() => {
      video.removeEventListener("resize", done);
      resolve(video.videoWidth > 0);
    }, 6000);
    const done = () => {
      if (video.videoWidth <= 0) return;
      window.clearTimeout(timer);
      video.removeEventListener("resize", done);
      resolve(true);
    };
    video.addEventListener("resize", done);
    if (video.videoWidth > 0) done();
  });
}

function forcePacketMode1(sdp: string) {
  return sdp.replaceAll("packetization-mode=0", "packetization-mode=1");
}

function scrubOfferSdp(sdp: string) {
  const fixed = forcePacketMode1(sdp);
  const keepMdns = firefoxUA() && isLoopbackOrigin();
  return fixed
    .split(/\r?\n/)
    .filter((line) => {
      if (!line.startsWith("a=candidate:")) return true;
      if (!keepMdns && line.includes(".local")) return false;
      if (/\s169\.254\./.test(line)) return false;
      if (/\s172\.(1[6-9]|2\d|3[01])\./.test(line)) return false;
      return true;
    })
  .join("\r\n");
}
