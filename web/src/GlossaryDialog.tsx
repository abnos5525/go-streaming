import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { glossFlow, glossLoci, glossTerms, type GlossTerm } from "@/glossary";

type Props = { onClose: () => void };

export function Glossary({ onClose }: Props) {
  const [query, setQuery] = useState("");
  const [picked, setPicked] = useState<GlossTerm | null>(null);

  const items = useMemo(() => {
    const q = query.trim().toLowerCase();
    return [...glossTerms]
      .filter((item) => !q || item.term.toLowerCase().includes(q) || item.def.includes(query.trim()))
      .sort((a, b) => a.term.localeCompare(b.term, "en", { sensitivity: "base" }));
  }, [query]);

  const focus = picked ? glossLoci[picked.term] : undefined;
  const hot = focus?.nodes[0];

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="gloss-bg"
      role="dialog"
      aria-modal="true"
      aria-labelledby="glossTitle"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="gloss-modal">
        <div className="gloss-head">
          <div>
            <h3 id="glossTitle">کلمات کلیدی</h3>
            <p className="sub">
              از فهرست راست یک واژه را بزن؛ در انیمیشن چپ همان ایستگاه روشن می‌شود و می‌بینی کجای فلو اجرا می‌شود.
            </p>
          </div>
          <Button type="button" onClick={onClose}>
            بستن
          </Button>
        </div>
        <div className="gloss-body">
          <div className="gloss-stage">
            <div className="gloss-stage-head">
              <h4>FULL FLOW · جزئی</h4>
              <span className="live-pill">
                <i />
                LIVE
              </span>
            </div>
            <div className="gloss-flow">
              {glossFlow.map((band) => (
                <div key={band.band} className={`gloss-band ${band.band}`}>
                  <div className="ttl">{band.title}</div>
                  <div className="gloss-rail">
                    {band.nodes.map((node, index) => {
                      const on = focus?.nodes.includes(node.id) ?? false;
                      return (
                        <span key={node.id} className="gloss-pair">
                          {index > 0 ? (
                            <div className={`gloss-wire${band.band === "ctrl" ? " cyan" : ""}${on ? " on" : ""}${node.id === hot ? " hot" : ""}`} />
                          ) : null}
                          <div className={`gloss-node${on ? " on" : ""}${node.id === hot ? " hot" : ""}`}>
                            <span className="kw-tag">{on && picked ? picked.term : ""}</span>
                            <b>{node.term}</b>
                            <s>{node.sub}</s>
                          </div>
                        </span>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
            <div className="gloss-callout" aria-live="polite">
              <div className="lab">{picked ? picked.term : "FOCUS"}</div>
              {picked ? (
                <div className="txt">
                  {picked.def}
                  <br />
                  <bdi className="en" dir="ltr">
                    {focus?.where ?? "مفهوم کلی در همین فلو"}
                  </bdi>
                </div>
              ) : (
                <div className="txt empty">یک کلمه از فهرست راست انتخاب کن تا محل اجرا در فلو مشخص شود.</div>
              )}
            </div>
          </div>
          <div className="gloss-keys">
            <div className="gloss-keys-bar">
              <input
                type="search"
                value={query}
                placeholder="جستجو در واژه‌ها…"
                autoComplete="off"
                onChange={(event) => setQuery(event.target.value)}
              />
              <span className="count">{items.length}</span>
            </div>
            <div className="gloss-list">
              {items.map((item) => (
                <button
                  key={item.term}
                  type="button"
                  className={`gloss-item${picked?.term === item.term ? " on" : ""}`}
                  onClick={() => setPicked(item)}
                >
                  <span className="term">{item.term}</span>
                  <span className="def">{item.def}</span>
                  {glossLoci[item.term] ? <span className="where">{glossLoci[item.term].where}</span> : null}
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
