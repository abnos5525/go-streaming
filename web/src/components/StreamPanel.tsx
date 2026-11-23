import { JsonTerminal } from "@/components/JsonTerminal";
import { LiveView } from "@/LiveView";

type StreamPanelProps = {
  origin: string;
  mediaURL: string;
  outputType: 6 | 7;
  playerURL: string;
  liveError: string;
  outputs: unknown;
  viewKey: string;
};

export function StreamPanel({
  origin,
  mediaURL,
  outputType,
  playerURL,
  liveError,
  outputs,
  viewKey,
}: StreamPanelProps) {
  return (
    <div className="stream-panel" aria-label="پخش زنده">
      <p className="prose stream-link">
        <a href={mediaURL}>{mediaURL}</a>
      </p>
      <div className="stream-video">
        {playerURL ? (
          <LiveView key={viewKey} origin={origin} mediaURL={mediaURL} outputType={outputType} />
        ) : null}
      </div>
      {liveError ? <p className="err">{liveError}</p> : null}
      <JsonTerminal
        title="outputs · ptz"
        hint="ptz:ack"
        value={outputs}
        placeholder="{ move joystick — ack appears here }"
      />
    </div>
  );
}