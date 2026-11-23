import { useCallback, useEffect, useRef, useState } from "react";
import { createPtzSocket, type PtzAck, type PtzSocket } from "@/ptzSocket";

const PAD_SIZE = 200;
const KNOB_SIZE = 48;
const KNOB_MAX = PAD_SIZE / 2 - KNOB_SIZE / 2 - 6;
const DEAD_ZONE = 0.08;
const KEEPALIVE_MS = 800;
const MIN_SEND_MS = 200;

type Vec = { x: number; y: number };

type Props = {
  apiOrigin: string;
  streamId: string;
  disabled?: boolean;
  streamLive?: boolean;
  onOutput: (payload: PtzAck) => void;
};

export function PtzJoystick({ apiOrigin, streamId, disabled = false, streamLive = false, onOutput }: Props) {
  const [wsUp, setWsUp] = useState(false);
  const [knob, setKnob] = useState<Vec>({ x: 0, y: 0 });
  const [zoomDir, setZoomDir] = useState(0);
  const [dragging, setDragging] = useState(false);
  const [invertX, setInvertX] = useState(false);
  const [invertY, setInvertY] = useState(false);

  const deviceId = streamId || "demo";
  const deviceIdRef = useRef(deviceId);
  deviceIdRef.current = deviceId;
  const invertRef = useRef({ x: false, y: false });
  const socketRef = useRef<PtzSocket | null>(null);
  const padRef = useRef<HTMLDivElement>(null);
  const vecRef = useRef<Vec>({ x: 0, y: 0 });
  const timerRef = useRef<number | null>(null);
  const draggingRef = useRef(false);
  const zoomDirRef = useRef(0);
  const lastSentRef = useRef({ x: 0, y: 0, z: 0, t: 0 });
  const interactive = !disabled && Boolean(streamId);

  const wireXY = useCallback((v: Vec) => {
    const inv = invertRef.current;
    return {
      x: inv.x ? -v.x : v.x,
      y: inv.y ? -v.y : v.y,
    };
  }, []);

  const syncKnobFromVec = useCallback(() => {
    const { x, y } = vecRef.current;
    setKnob({ x: x * KNOB_MAX, y: -y * KNOB_MAX });
  }, []);

  const forwardAck = useCallback(
    (ack: PtzAck) => {
      onOutput(ack);
    },
    [onOutput],
  );

  useEffect(() => {
    if (!streamId) {
      setWsUp(false);
      socketRef.current?.disconnect();
      socketRef.current = null;
      return;
    }
    const s = createPtzSocket(apiOrigin, streamId);
    const on = () => setWsUp(true);
    const off = () => setWsUp(false);
    const onErr = () => setWsUp(false);
    s.on("connect", on);
    s.on("disconnect", off);
    s.on("connect_error", onErr);
    s.connect();
    socketRef.current = s;
    return () => {
      try {
        s.emit("ptz:move", { x: 0, y: 0, zoom: 0, deviceId: deviceIdRef.current }, forwardAck);
      } catch {
        /* ignore */
      }
      s.off("connect", on);
      s.off("disconnect", off);
      s.off("connect_error", onErr);
      s.disconnect();
      socketRef.current = null;
    };
  }, [apiOrigin, streamId, forwardAck]);

  useEffect(() => {
    if (!wsUp) return;
    socketRef.current?.emit("ptz:status", { deviceId: deviceIdRef.current }, forwardAck);
  }, [wsUp, forwardAck]);

  const clearTimer = useCallback(() => {
    if (timerRef.current != null) {
      window.clearInterval(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const emitMove = useCallback(
    (v: Vec, zoom: number) => {
      const { x, y } = wireXY(v);
      socketRef.current?.emit(
        "ptz:move",
        { x, y, zoom, deviceId: deviceIdRef.current },
        forwardAck,
      );
    },
    [forwardAck, wireXY],
  );

  const sendHeld = useCallback(
    (keepalive: boolean) => {
      const v = vecRef.current;
      const z = zoomDirRef.current;
      const prev = lastSentRef.current;
      const same =
        Math.abs(v.x - prev.x) < DEAD_ZONE &&
        Math.abs(v.y - prev.y) < DEAD_ZONE &&
        Math.abs(z - prev.z) < DEAD_ZONE;
      const age = Date.now() - prev.t;
      if (!keepalive && same) return;
      if (!keepalive && age < MIN_SEND_MS) return;
      if (keepalive && same && age < KEEPALIVE_MS) return;
      lastSentRef.current = { x: v.x, y: v.y, z, t: Date.now() };
      emitMove(v, z);
    },
    [emitMove],
  );

  const bumpSend = useCallback(() => {
    lastSentRef.current = { x: 0, y: 0, z: -1, t: 0 };
    sendHeld(false);
  }, [sendHeld]);

  const startLoop = useCallback(() => {
    if (timerRef.current == null) {
      const v = vecRef.current;
      const z = zoomDirRef.current;
      lastSentRef.current = { x: v.x, y: v.y, z, t: Date.now() };
      emitMove(v, z);
      timerRef.current = window.setInterval(() => sendHeld(true), KEEPALIVE_MS);
      return;
    }
    sendHeld(false);
  }, [emitMove, sendHeld]);

  const stopAll = useCallback(() => {
    clearTimer();
    vecRef.current = { x: 0, y: 0 };
    zoomDirRef.current = 0;
    setZoomDir(0);
    socketRef.current?.emit(
      "ptz:move",
      { x: 0, y: 0, zoom: 0, deviceId: deviceIdRef.current },
      forwardAck,
    );
  }, [clearTimer, forwardAck]);

  useEffect(() => {
    stopAll();
  }, [deviceId, stopAll]);

  useEffect(() => {
    if (disabled) stopAll();
  }, [disabled, stopAll]);

  useEffect(
    () => () => {
      clearTimer();
    },
    [clearTimer],
  );

  const updateFromPoint = useCallback((clientX: number, clientY: number) => {
    const el = padRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    let dx = clientX - (rect.left + rect.width / 2);
    let dy = clientY - (rect.top + rect.height / 2);
    const dist = Math.hypot(dx, dy);
    if (dist > KNOB_MAX) {
      dx = (dx / dist) * KNOB_MAX;
      dy = (dy / dist) * KNOB_MAX;
    }
    let nx = dx / KNOB_MAX;
    let ny = -dy / KNOB_MAX;
    if (Math.abs(nx) < DEAD_ZONE) nx = 0;
    if (Math.abs(ny) < DEAD_ZONE) ny = 0;
    vecRef.current = { x: nx, y: ny };
    syncKnobFromVec();
  }, [syncKnobFromVec]);

  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      if (!interactive) return;
      e.preventDefault();
      e.currentTarget.setPointerCapture(e.pointerId);
      draggingRef.current = true;
      setDragging(true);
      updateFromPoint(e.clientX, e.clientY);
      startLoop();
    },
    [interactive, startLoop, updateFromPoint],
  );

  const onPointerMove = useCallback(
    (e: React.PointerEvent) => {
      if (!draggingRef.current) return;
      updateFromPoint(e.clientX, e.clientY);
      startLoop();
    },
    [startLoop, updateFromPoint],
  );

  const onPointerEnd = useCallback(() => {
    if (!draggingRef.current) return;
    draggingRef.current = false;
    setDragging(false);
    setKnob({ x: 0, y: 0 });
    clearTimer();
    vecRef.current = { x: 0, y: 0 };
    socketRef.current?.emit(
      "ptz:move",
      { x: 0, y: 0, zoom: zoomDirRef.current, deviceId: deviceIdRef.current },
      forwardAck,
    );
  }, [clearTimer, forwardAck]);

  const applyZoom = useCallback(
    (v: number) => {
      const clamped = Math.max(-1, Math.min(1, v));
      zoomDirRef.current = clamped;
      setZoomDir(clamped);
      if (clamped !== 0) startLoop();
      else {
        clearTimer();
        emitMove(vecRef.current, 0);
      }
    },
    [startLoop, clearTimer, emitMove],
  );

  const resetZoom = useCallback(() => {
    clearTimer();
    zoomDirRef.current = 0;
    setZoomDir(0);
    socketRef.current?.emit("ptz:absolute", { zoom: 0, deviceId: deviceIdRef.current }, forwardAck);
  }, [clearTimer, forwardAck]);

  let cursor = "grab";
  if (disabled || !streamId) cursor = "not-allowed";
  else if (dragging) cursor = "grabbing";

  return (
    <aside className="ptz-rail" aria-label="PTZ joystick">
      <div className="ptz-rail-head">
        <span className={`ptz-dot${streamLive ? " on" : " off"}`} />
        <span className="ptz-rail-title">PTZ</span>
        <span className={`ptz-rail-state${streamLive ? " live" : " dead"}`}>{streamLive ? "متصل" : "قطع"}</span>
      </div>
      <div className="ptz-controls">
        <div
          ref={padRef}
          dir="ltr"
          className="ptz-pad"
          style={{ cursor }}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerEnd}
          onPointerCancel={onPointerEnd}
        >
          <span className="ptz-cross h" aria-hidden />
          <span className="ptz-cross v" aria-hidden />
          <span
            className={`ptz-knob${dragging ? " on" : ""}`}
            style={{ transform: `translate(${knob.x}px, ${knob.y}px)` }}
          />
        </div>
        <div className="ptz-zoom">
          <span className="ptz-zoom-icon" aria-hidden>
            +
          </span>
          <input
            type="range"
            className="ptz-zoom-slider"
            min={-1}
            max={1}
            step={1}
            value={zoomDir}
            disabled={!interactive}
            onChange={(e) => applyZoom(Number(e.target.value))}
            onPointerUp={() => applyZoom(0)}
            aria-label="zoom"
          />
          <span className="ptz-zoom-icon" aria-hidden>
            −
          </span>
          <button type="button" className="ptz-zoom-reset" onClick={resetZoom} disabled={!interactive} title="بازگشت زوم">
            ↺
          </button>
        </div>
      </div>
      <div className="ptz-toggles">
        <label>
          <input
            type="checkbox"
            checked={invertX}
            onChange={() => {
              const next = !invertRef.current.x;
              invertRef.current = { ...invertRef.current, x: next };
              setInvertX(next);
              bumpSend();
            }}
            disabled={!interactive}
          />
          معکوس افقی
        </label>
        <label>
          <input
            type="checkbox"
            checked={invertY}
            onChange={() => {
              const next = !invertRef.current.y;
              invertRef.current = { ...invertRef.current, y: next };
              setInvertY(next);
              bumpSend();
            }}
            disabled={!interactive}
          />
          معکوس عمودی
        </label>
      </div>
    </aside>
  );
}