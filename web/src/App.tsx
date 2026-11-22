import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Glossary } from "@/GlossaryDialog";

type BootStep = { step: string; name: string; detail: string };

const boot: BootStep[] = [
  { step: "قدم ۱", name: "config.Load()", detail: "خواندن .env و آدرس گوش دادن" },
  { step: "قدم ۲", name: "gin + CORS", detail: "ساخت روتر و باز کردن مرورگر روی origin دیگر" },
  { step: "قدم ۳", name: "Listen :8085", detail: "آماده برای LL-HLS و WebRTC" },
];

export default function App() {
  const [inside, setInside] = useState(false);
  const [gloss, setGloss] = useState(false);

  return (
    <>
      <header>
        <div>
          <h1>فلو زندهٔ go-streaming</h1>
          <p>
            {inside
              ? "بوت سرور · سه قدم اول تا Listen"
              : "نقشهٔ کلی سیستم · برای ورود روی go-streaming کلیک کن"}
          </p>
        </div>
        {inside ? (
          <Button type="button" onClick={() => setInside(false)}>
            ↩ برگشت به نقشه
          </Button>
        ) : null}
      </header>
      <main>
        <section id="world" className={inside ? "scene" : "scene on"} aria-label="نقشه کلی">
          <div className="hero">
            <p className="hero-kicker">جریان واقعی محصول شما</p>
            <div className="flow-row">
              <article className="card">
                <h2>Record Server</h2>
                <span>C++ · RTP / UDP</span>
              </article>
              <div className="conn" aria-hidden="true">
                <span className="tag">RTP</span>
                <i />
              </div>
              <article
                className="card main"
                tabIndex={0}
                role="button"
                aria-label="ورود به داخل go-streaming"
                onClick={() => setInside(true)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    setInside(true);
                  }
                }}
              >
                <h2>go-streaming</h2>
                <span>Go · این پروژه</span>
                <span className="cta">برای ورود به داخل، کلیک کن</span>
              </article>
              <div className="conn cyan" aria-hidden="true">
                <span className="tag">WebRTC</span>
                <i />
              </div>
              <article className="card">
                <h2>React</h2>
                <span>Browser · پخش</span>
              </article>
            </div>
            <ul className="legend">
              <li>
                <span className="dot a" />
                خط زرد = پکت ویدیو از Record
              </li>
              <li>
                <span className="dot c" />
                خط آبی = خروجی به مرورگر
              </li>
            </ul>
            <div className="hero-actions">
              <Button
                type="button"
                className="btn gloss-open"
                title="واژه‌نامه + انیمیشن فلو"
                onClick={() => setGloss(true)}
              >
                کلمات کلیدی
              </Button>
            </div>
            <p className="world-hint">
              الان فقط نقشهٔ بیرون را می‌بینی. برای دیدن فایل‌ها و متدها روی <b>go-streaming</b> کلیک کن. واژه‌ها را از دکمهٔ بالا مرور کن.
            </p>
          </div>
        </section>
        <section id="inside" className={inside ? "scene on" : "scene"} aria-label="بوت سرور">
          <div className="boot">
            {boot.map((item) => (
              <article key={item.name}>
                <em>{item.step}</em>
                <div>
                  <b>{item.name}</b>
                  <span>{item.detail}</span>
                </div>
              </article>
            ))}
          </div>
        </section>
      </main>
      {gloss ? <Glossary onClose={() => setGloss(false)} /> : null}
    </>
  );
}
