import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Glossary } from "@/GlossaryDialog";
import { Inside } from "@/Inside";
import { Scenario } from "@/Scenario";

type View = "map" | "inside" | "scenario";

export default function App() {
  const [view, setView] = useState<View>("map");
  const [gloss, setGloss] = useState(false);

  return (
    <>
      <header>
        <div>
          <h1>فلو زندهٔ go-streaming</h1>
          <p>
            {view === "inside"
              ? "داخل go-streaming · قدم‌ها و فایل‌ها"
              : view === "scenario"
                ? "سناریوی پخش · Start و Stop روی API"
                : "نقشهٔ کلی سیستم · برای ورود روی go-streaming کلیک کن"}
          </p>
        </div>
        {view !== "map" ? (
          <Button type="button" onClick={() => setView("map")}>
            ↩ برگشت به نقشه
          </Button>
        ) : null}
      </header>
      <main>
        <section id="world" className={view === "map" ? "scene on" : "scene"} aria-label="نقشه کلی">
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
                onClick={() => setView("inside")}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    setView("inside");
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
              <Button type="button" className="btn primary" onClick={() => setView("scenario")}>
                سناریوی پخش
              </Button>
            </div>
            <p className="world-hint">
              الان فقط نقشهٔ بیرون را می‌بینی. برای دیدن فایل‌ها و متدها روی <b>go-streaming</b> کلیک کن. واژه‌ها را از دکمهٔ بالا مرور کن.
            </p>
          </div>
        </section>
        {view === "inside" ? <Inside /> : null}
        {view === "scenario" ? <Scenario /> : null}
      </main>
      {gloss ? <Glossary onClose={() => setGloss(false)} /> : null}
    </>
  );
}
