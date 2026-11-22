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
    if (outputType === 6) {
      if (Hls.isSupported()) {
        const player = new Hls({ lowLatencyMode: true, enableWorker: false });
        player.loadSource(mediaURL);
        player.attachMedia(video);
        player.on(Hls.Events.ERROR, (_, data) => {
          if (!data.fatal) return;
          if (data.type === Hls.ErrorTypes.MEDIA_ERROR) player.recoverMediaError();
          else if (data.type === Hls.ErrorTypes.NETWORK_ERROR) player.startLoad();
        });
        return () => player.destroy();
      }
      video.src = mediaURL;
      return;
    }

    let closed = false;
    let pc: RTCPeerConnection | null = null;
    void (async () => {
      const viewers = await fetch(`${origin}/webrtc/${id}/viewers`, { method: "POST" });
      if (!viewers.ok || closed) return;
      const slot = (await viewers.json()) as Slot;
      pc = new RTCPeerConnection({ iceServers: [] });
      pc.addTransceiver("video", { direction: "recvonly" });
      pc.ontrack = (event) => {
        video.srcObject = event.streams[0] ?? new MediaStream([event.track]);
        void video.play().catch(() => undefined);
      };
      pc.createDataChannel(slot.channel, { negotiated: true, id: slot.dc_id });
      await pc.setLocalDescription(await pc.createOffer());
      await new Promise((resolve) => window.setTimeout(resolve, 800));
      if (closed || !pc.localDescription) return;
      const offerURL = slot.offer.startsWith("http") ? slot.offer : `${origin}${slot.offer}`;
      const answer = await fetch(offerURL, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ sdp: pc.localDescription.sdp, type: pc.localDescription.type }),
      });
      if (!answer.ok || closed) return;
      await pc.setRemoteDescription(await answer.json());
    })();
    return () => {
      closed = true;
      pc?.close();
    };
  }, [id, mediaURL, origin, outputType]);

  return <video ref={videoRef} className="live" autoPlay playsInline muted controls />;
}
