// CLI output in the CLI's own colours. Each line is plain text with spans
// marked «b|bold», «d|dim», «r|exposed red» or «g|green check». A line that
// starts with "$ " is drawn as a prompt. The same markers drive the
// terminals on muretto.ai.
export const Terminal = ({ title = "muretto", lines = [] }) => {
  const spans = (line) => {
    const out = [];
    const re = /«([bdrg])\|([^»]*)»/g;
    let last = 0;
    let m;
    while ((m = re.exec(line)) !== null) {
      if (m.index > last) out.push(line.slice(last, m.index));
      out.push(
        <span key={m.index} className={"mu-t-" + m[1]}>
          {m[2]}
        </span>,
      );
      last = re.lastIndex;
    }
    if (last < line.length) out.push(line.slice(last));
    return out;
  };
  return (
    <div className="mu-term not-prose">
      <div className="mu-term-bar" aria-hidden="true">
        <span className="mu-term-dot" />
        <span className="mu-term-dot" />
        <span className="mu-term-dot" />
        <span className="mu-term-title">{title}</span>
      </div>
      <pre className="mu-term-body">
        <code>
          {lines.map((line, i) =>
            line.startsWith("$ ") ? (
              <span key={i}>
                <span className="mu-t-p">$</span> <span className="mu-t-c">{line.slice(2)}</span>
                {"\n"}
              </span>
            ) : (
              <span key={i}>
                {spans(line)}
                {"\n"}
              </span>
            ),
          )}
        </code>
      </pre>
    </div>
  );
};
