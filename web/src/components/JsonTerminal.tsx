type JsonTerminalProps = {
  title: string;
  hint?: string;
  value?: unknown;
  raw?: string;
  placeholder?: string;
};

function pretty(value: unknown, raw?: string): { text: string; valid: boolean } {
  if (value !== undefined) {
    try {
      return { text: JSON.stringify(value, null, 2), valid: true };
    } catch {
      return { text: String(value), valid: false };
    }
  }
  const trim = raw?.trim() ?? "";
  if (!trim) return { text: "", valid: true };
  try {
    return { text: JSON.stringify(JSON.parse(trim), null, 2), valid: true };
  } catch {
    return { text: trim, valid: false };
  }
}

function tokenize(line: string): { text: string; kind: string }[] {
  const tokens: { text: string; kind: string }[] = [];
  const re = /"(?:\\.|[^"\\])*"(\s*:)?|\b(?:true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|[{}\[\],]/g;
  let last = 0;
  let hit: RegExpExecArray | null;
  while ((hit = re.exec(line)) !== null) {
    if (hit.index > last) tokens.push({ text: line.slice(last, hit.index), kind: "plain" });
    const chunk = hit[0];
    let kind = "num";
    if (chunk.startsWith('"')) kind = /:\s*$/.test(chunk) ? "key" : "str";
    else if (chunk === "true" || chunk === "false") kind = "bool";
    else if (chunk === "null") kind = "null";
    else if (/^[\[{}\],]$/.test(chunk)) kind = "punct";
    tokens.push({ text: chunk, kind });
    last = hit.index + hit[0].length;
  }
  if (last < line.length) tokens.push({ text: line.slice(last), kind: "plain" });
  return tokens;
}

export function JsonTerminal({ title, hint, value, raw, placeholder }: JsonTerminalProps) {
  const { text, valid } = pretty(value, raw);
  const showPlaceholder = !text && placeholder;

  return (
    <div className={`json-terminal${showPlaceholder ? " json-terminal--idle" : ""}`} dir="ltr">
      <header className="json-terminal-head">
        <span className="json-terminal-dots" aria-hidden>
          <i />
          <i />
          <i />
        </span>
        <span className="json-terminal-title">{title}</span>
        {hint ? <span className="json-terminal-hint">{hint}</span> : null}
        {!valid ? <span className="json-terminal-badge">raw</span> : null}
      </header>
      <pre className="json-terminal-body" tabIndex={0}>
        {showPlaceholder ? (
          <span className="json-terminal-placeholder">{placeholder}</span>
        ) : (
          text.split("\n").map((line, row) => (
            <span key={row} className="json-line">
              {tokenize(line).map((part, col) => (
                <span key={col} className={`json-token json-${part.kind}`}>
                  {part.text}
                </span>
              ))}
              {"\n"}
            </span>
          ))
        )}
      </pre>
    </div>
  );
}