export type PtzBody = {
  deviceId?: string;
  x?: number;
  y?: number;
  zoom?: number;
};

export type PtzAck = {
  type?: string;
  direction: string;
  pan?: number;
  tilt?: number;
  zoom?: number;
  deviceId?: string | null;
  streamId?: string;
};

export type PtzEmitEvent = "ptz:move" | "ptz:status" | "ptz:absolute";

type Fn = () => void;
type ErrFn = (err: Error) => void;
type AckFn = (r: PtzAck) => void;

export type PtzSocket = {
  connected: boolean;
  connect: () => void;
  disconnect: () => void;
  on: (event: "connect" | "disconnect" | "connect_error", fn: Fn | ErrFn) => void;
  off: (event: "connect" | "disconnect" | "connect_error", fn: Fn | ErrFn) => void;
  emit: (event: PtzEmitEvent, body: PtzBody, ack?: AckFn) => void;
};

function ptzWsURL(apiOrigin: string, streamId: string): string {
  const base = new URL(apiOrigin, window.location.origin);
  base.protocol = base.protocol === "https:" ? "wss:" : "ws:";
  base.pathname = `${base.pathname.replace(/\/$/, "")}/ptz`;
  base.search = streamId ? `id=${encodeURIComponent(streamId)}` : "";
  base.hash = "";
  return base.toString();
}

export function createPtzSocket(apiOrigin: string, streamId: string): PtzSocket {
  let ws: WebSocket | null = null;
  let open = false;
  let latestAck: AckFn | null = null;
  const connectFns = new Set<Fn>();
  const disconnectFns = new Set<Fn>();
  const errorFns = new Set<ErrFn>();

  const sock: PtzSocket = {
    get connected() {
      return open;
    },
    connect() {
      if (!streamId) return;
      if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
        if (open) connectFns.forEach((f) => f());
        return;
      }
      const socket = new WebSocket(ptzWsURL(apiOrigin, streamId));
      ws = socket;
      socket.onopen = () => {
        if (ws !== socket) return;
        open = true;
        connectFns.forEach((f) => f());
      };
      socket.onclose = () => {
        if (ws !== socket) return;
        open = false;
        latestAck = null;
        disconnectFns.forEach((f) => f());
      };
      socket.onerror = () => {
        errorFns.forEach((f) => f(new Error("ptz websocket")));
      };
      socket.onmessage = (ev) => {
        try {
          const msg = JSON.parse(String(ev.data)) as PtzAck;
          if (msg.type !== "ptz:ack") return;
          const fn = latestAck;
          latestAck = null;
          fn?.(msg);
        } catch {
          /* ignore */
        }
      };
    },
    disconnect() {
      const socket = ws;
      ws = null;
      open = false;
      latestAck = null;
      if (socket && socket.readyState !== WebSocket.CLOSED) socket.close();
    },
    on(event, fn) {
      if (event === "connect") connectFns.add(fn as Fn);
      if (event === "disconnect") disconnectFns.add(fn as Fn);
      if (event === "connect_error") errorFns.add(fn as ErrFn);
    },
    off(event, fn) {
      if (event === "connect") connectFns.delete(fn as Fn);
      if (event === "disconnect") disconnectFns.delete(fn as Fn);
      if (event === "connect_error") errorFns.delete(fn as ErrFn);
    },
    emit(event, body, ack) {
      if (!ws || ws.readyState !== WebSocket.OPEN) return;
      if (ack) latestAck = ack;
      const payload =
        event === "ptz:status"
          ? { type: "ptz:status", deviceId: body.deviceId }
          : event === "ptz:absolute"
            ? { type: "ptz:absolute", zoom: body.zoom ?? 0, deviceId: body.deviceId }
            : {
                type: "ptz:move",
                x: body.x ?? 0,
                y: body.y ?? 0,
                zoom: body.zoom ?? 0,
                deviceId: body.deviceId,
              };
      ws.send(JSON.stringify(payload));
    },
  };
  return sock;
}