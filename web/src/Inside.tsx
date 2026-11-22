import { useState } from "react";
import { Button } from "@/components/ui/button";
import { steps } from "@/steps";

const media = ["rec", "udp", "sniff", "fan", "q", "hub", "react"] as const;
const control = ["http", "api", "sess", "lane", "sig"] as const;

const mediaOn: Record<number, string[]> = {
  0: [],
  1: [],
  2: [],
  3: ["rec"],
  4: ["rec", "udp"],
  5: ["rec", "udp"],
  6: ["rec", "udp", "sniff"],
  7: ["udp", "sniff", "fan"],
  8: ["sniff", "fan", "q"],
  9: ["fan", "q", "hub"],
  10: ["q", "hub", "react"],
  11: ["hub", "react"],
  12: [],
};

const controlOn: Record<number, string[]> = {
  0: [],
  1: ["http"],
  2: ["http", "api"],
  3: ["http", "api", "sess"],
  4: ["api", "sess"],
  5: ["sess", "lane"],
  6: ["sess", "lane"],
  7: ["lane"],
  8: ["lane"],
  9: ["lane"],
  10: ["lane", "sig"],
  11: ["http", "sig", "lane"],
  12: ["http", "api"],
};

const mediaLabel: Record<(typeof media)[number], [string, string]> = {
  rec: ["Record", "C++ RTP"],
  udp: ["UDP", "socket"],
  sniff: ["sniff", "Read"],
  fan: ["fanout", "copy"],
  q: ["rtcQ", "queue"],
  hub: ["Hub", "peers"],
  react: ["React", "<video>"],
};

const controlLabel: Record<(typeof control)[number], [string, string]> = {
  http: ["HTTP", ":8085"],
  api: ["API", "/start"],
  sess: ["session", "map"],
  lane: ["lane", "WebRTC"],
  sig: ["offer", "answer"],
};

const mediaWire = ["RTP", "datagram", "datagram", "[]byte", "[]byte", "WebRTC"];
const controlWire = ["body", "(url,7)", "lane", "SDP"];

function Row({
  ids,
  labels,
  wires,
  on,
  cyan,
}: {
  ids: readonly string[];
  labels: Record<string, [string, string]>;
  wires: string[];
  on: string[];
  cyan: boolean;
}) {
  return (
    <div className="live-row">
      {ids.map((id, index) => {
        const lit = on.includes(id);
        const wireLit = lit && on.includes(ids[index - 1] ?? "");
        return (
          <span key={id} className="live-pair">
            {index > 0 ? (
              <div className={`lwire${cyan ? " cyan" : ""}${wireLit ? " on flowing" : ""}`}>
                <span className="cargo">{wires[index - 1]}</span>
              </div>
            ) : null}
            <div className={`lnode${lit ? " on hot" : ""}`}>
              <b>{labels[id][0]}</b>
              <s>{labels[id][1]}</s>
            </div>
          </span>
        );
      })}
    </div>
  );
}

export function Inside() {
  const [index, setIndex] = useState(0);
  const step = steps[index];
  const progress = ((index + 1) / steps.length) * 100;

  return (
    <section id="inside" className="scene on" aria-label="فایل‌ها و متدها">
      <aside className="viz" aria-label="انیمیشن فلو">
        <h3>LIVE SYSTEM · جریان واقعی</h3>
        <div className="pipe">
          <div className="live-plane media">
            <header>
              <span>MEDIA PLANE</span>
              <em>Record → UDP → sniff → rtcQ → hub → React</em>
            </header>
            <Row
              ids={media}
              labels={mediaLabel}
              wires={mediaWire}
              on={mediaOn[step.phase] ?? []}
              cyan={false}
            />
          </div>
          <div className="live-plane ctrl">
            <header>
              <span>CONTROL PLANE</span>
              <em>HTTP :8085 · Start / offer</em>
            </header>
            <Row
              ids={control}
              labels={controlLabel}
              wires={controlWire}
              on={controlOn[step.phase] ?? []}
              cyan
            />
          </div>
          <p className="live-status">
            الان روی: <b>{step.title}</b>
            <span className="now">{step.file}</span>
          </p>
          <ul className="live-legend">
            <li>
              <i className="a" />
              زرد = پکت ویدیو / RTP
            </li>
            <li>
              <i className="c" />
              آبی = کنترل HTTP یا صف WebRTC
            </li>
          </ul>
        </div>
        <h3 className="fa">انیمیشن همین قدم</h3>
        <div className="micro">
          <div className="micro-scroll">
            <p className="story">{step.plain}</p>
            <ol className="teach">
              {step.how.map((line) => (
                <li key={line} className="teach-step on">
                  <span className="body">{line}</span>
                </li>
              ))}
            </ol>
            <p className="note">{step.next}</p>
          </div>
        </div>
      </aside>
      <aside className="rail">
        <h3>قدم‌ها</h3>
        <div>
          {steps.map((item, i) => (
            <button
              key={item.title}
              type="button"
              className={`step${i === index ? " on" : ""}`}
              onClick={() => setIndex(i)}
            >
              <b>{item.title}</b>
              <i>{item.method}</i>
            </button>
          ))}
        </div>
        <div className="flowbar">
          <i style={{ width: `${progress}%` }} />
        </div>
      </aside>
      <div className="panel">
        <div className="crumb">
          {index + 1} / {steps.length}
        </div>
        <h2>{step.title}</h2>
        <div className="meta">
          <span className="chip">
            <em>file</em> {step.file}
          </span>
          <span className="chip">
            <em>method</em> {step.method}
          </span>
        </div>
        <div className="box key">
          <h4>مفهوم کلیدی</h4>
          <p className="prose">{step.key}</p>
        </div>
        <div className="box">
          <h4>به زبان ساده</h4>
          <p className="prose">{step.plain}</p>
        </div>
        <div className="box">
          <h4>جزئیات فنی</h4>
          <ul>
            {step.how.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
        </div>
        <div className="box">
          <h4>چرا این طراحی؟</h4>
          <p className="prose">{step.why}</p>
        </div>
        <div className="box">
          <h4>بعدش</h4>
          <p className="prose">{step.next}</p>
        </div>
        <div className="nav">
          <Button type="button" disabled={index === 0} onClick={() => setIndex(index - 1)}>
            قبلی
          </Button>
          <Button
            type="button"
            variant="primary"
            className="btn primary"
            disabled={index === steps.length - 1}
            onClick={() => setIndex(index + 1)}
          >
            بعدی →
          </Button>
        </div>
      </div>
    </section>
  );
}
