import { JsonTerminal } from "@/components/JsonTerminal";
import { LiveView } from "@/LiveView";

export type SlidePane = {
  key: string;
  mediaURL: string;
  outputType: 6 | 7;
  active: boolean;
};

type StreamPanelProps = {
  origin: string;
  mediaURL: string;
  panes: SlidePane[];
  playerURL: string;
  liveError: string;
  outputs: unknown;
  slide?: { label: string; onPrev: () => void; onNext: () => void };
};

export function StreamPanel({
  origin,
  mediaURL,
  panes,
  playerURL,
  liveError,
  outputs,
  slide,
}: StreamPanelProps) {
  return (
    <div className="stream-panel" aria-label="پخش زنده">
      <p className="prose stream-link">
        <a href={mediaURL}>{mediaURL}</a>
      </p>
      <div className="stream-video">
        {playerURL
          ? panes.map((pane) => (
              <div key={pane.key} className={pane.active ? "slide-pane on" : "slide-pane"}>
                <LiveView
                  origin={origin}
                  mediaURL={pane.mediaURL}
                  outputType={pane.outputType}
                  active={pane.active}
                />
              </div>
            ))
          : null}
        {slide ? (
          <>
            <button type="button" className="slide-nav prev" onClick={slide.onPrev} aria-label="دوربین قبلی">
              ‹
            </button>
            <button type="button" className="slide-nav next" onClick={slide.onNext} aria-label="دوربین بعدی">
              ›
            </button>
            <span className="slide-label">{slide.label}</span>
          </>
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