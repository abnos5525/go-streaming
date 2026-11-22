import { useEffect, useRef } from "react";
import Hls from "hls.js";

type Slot = { offer: string; channel: string; dc_id: number };

export function LiveView({
  origin,
  mediaURL,
  outputType,
}: {
  origin: string;
  mediaURL: string;
  outputType: 6 | 7;
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
    void (async () => {
      const viewers = await fetch(`${origin}/webrtc/${id}/viewers`, { method: "POST" });
      if (!viewers.ok || closed) return;
      const slot = (await viewers.json()) as Slot;
      pc = new RTCPeerConnection({ iceServers: [] });
      pc.createDataChannel(slot.channel, { negotiated: true, id: slot.dc_id });
      pc.ontrack = (event) => {
        const receiver = event.receiver as RTCRtpReceiver & { playoutDelayHint?: number };
        try {
          receiver.jitterBufferTarget = 200;
          receiver.playoutDelayHint = 0.2;
        } catch {
          return;
        }
        video.srcObject = event.streams[0] ?? new MediaStream([event.track]);
        arm();
      };
      pc.addTransceiver("video", { direction: "recvonly" });
      preferMainH264(pc);
      await pc.setLocalDescription(await pc.createOffer());
      await waitForHost(pc);
      if (closed || !pc.localDescription) return;
      const offerURL = slot.offer.startsWith("http") ? slot.offer : `${origin}${slot.offer}`;
      const answer = await fetch(offerURL, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ sdp: pc.localDescription.sdp, type: pc.localDescription.type }),
      });
      if (!answer.ok || closed) return;
      await pc.setRemoteDescription(await answer.json());
      arm();
    })();
    return () => {
      closed = true;
      pc?.close();
    };
  }, [id, mediaURL, origin, outputType]);

  return <video ref={videoRef} className="live" autoPlay playsInline muted controls />;
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

function waitForHost(pc: RTCPeerConnection) {
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
        if (candidate.includes("typ host") && !candidate.includes(".local")) {
          pc.removeEventListener("icecandidate", hit);
          resolve();
        }
      };
      pc.addEventListener("icecandidate", hit);
    }),
    new Promise<void>((resolve) => window.setTimeout(resolve, 800)),
  ]);
}
