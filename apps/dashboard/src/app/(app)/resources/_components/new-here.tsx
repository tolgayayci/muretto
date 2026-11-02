// My resources before anything is connected: the four commands and a small example.
import { Badge, Icon } from "@/components/ds";
import { KindIcon, ProjectBadge, ProviderBadge, SessionAvatar } from "@/components/brand";
import { CommandChip } from "@/components/client";
import { PageHeader } from "@/components/ui";

const STEPS = [
  { title: "Install the CLI", note: "Or with Homebrew: brew install tolgayayci/tap/muretto", cmd: "npm i -g muretto" },
  { title: "Add the hooks and connect providers", note: "Supabase and Vercel, with read-only tokens.", cmd: "muretto init" },
  { title: "Sign the CLI in", note: "You approve a short code in this browser.", cmd: "muretto login" },
  { title: "Turn on sync", note: "Shows every field before anything is sent.", cmd: "muretto sync on" },
];

const FACTS = [
  ["search", "Everything in your accounts, oldest first."],
  ["sparkle", "Which agent session made it, or “origin unknown”."],
  ["lock", "Muretto never deletes. You decide what to keep."],
] as const;

const exRow = { display: "grid", gridTemplateColumns: "30px minmax(0, 1fr) auto", gap: 12, alignItems: "center", padding: "12px 14px", borderBottom: "1px solid var(--line)" } as const;
const exTile = { width: 30, height: 30, borderRadius: 8 } as const;
const exSub = { display: "flex", alignItems: "center", gap: 10, fontSize: 12, color: "var(--ink-faint)", whiteSpace: "nowrap" } as const;

export function NewHere() {
  return (
    <div className="m-page" style={{ gap: 32 }}>
      <PageHeader eyebrow="My resources" title="Nothing to list yet." quiet="Connect a machine and your accounts first." />
      <div className="m-split">
        <section aria-labelledby="steps-h" className="m-card" style={{ borderRadius: "var(--radius-2xl)", padding: 26, display: "flex", flexDirection: "column", gap: 6 }}>
          <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 12, paddingBottom: 10 }}>
            <h2 id="steps-h" style={{ margin: 0, fontSize: 18, lineHeight: "24px", fontWeight: 600 }}>
              Four commands, about a minute
            </h2>
            <span className="m-note-mono">macOS · Linux</span>
          </div>
          <ol style={{ margin: 0, padding: 0, listStyle: "none", display: "flex", flexDirection: "column" }}>
            {STEPS.map((st, i) => (
              <li key={st.cmd} style={{ display: "grid", gridTemplateColumns: "28px minmax(0, 1fr)", gap: 14, padding: "14px 0", borderTop: "1px solid var(--line)" }}>
                <span style={{ width: 26, height: 26, boxSizing: "border-box", borderRadius: 999, border: "1px solid var(--line-strong)", display: "inline-flex", alignItems: "center", justifyContent: "center", font: "600 12px var(--font-mono)", color: "var(--ink-muted)" }}>{i + 1}</span>
                <span style={{ display: "flex", flexDirection: "column", gap: 8, minWidth: 0 }}>
                  <span style={{ display: "flex", flexDirection: "column", gap: 2 }}>
                    <span style={{ fontSize: 15, fontWeight: 600 }}>{st.title}</span>
                    <span style={{ fontSize: 13.5, color: "var(--ink-faint)" }}>{st.note}</span>
                  </span>
                  <CommandChip command={st.cmd} />
                </span>
              </li>
            ))}
          </ol>
          <div style={{ marginTop: "auto", display: "flex", alignItems: "center", gap: 10, padding: "14px 16px", border: "1px dashed var(--line-strong)", borderRadius: "var(--radius-lg)", fontSize: 14, color: "var(--ink-muted)" }}>
            <span className="m-dot off" style={{ width: 8, height: 8, boxShadow: "0 0 0 4px var(--surface-hover)" }} />
            Waiting for your first machine. Reload after the first sync.
          </div>
        </section>
        <section aria-labelledby="see-h" className="m-card" style={{ borderRadius: "var(--radius-2xl)", background: "var(--surface-sunken)", padding: 26, display: "flex", flexDirection: "column", gap: 18 }}>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12 }}>
            <h2 id="see-h" style={{ margin: 0, fontSize: 18, lineHeight: "24px", fontWeight: 600 }}>
              What you&apos;ll see here
            </h2>
            <span className="m-sev" style={{ color: "var(--ink-faint)" }}>
              example
            </span>
          </div>
          <div aria-hidden style={{ display: "flex", flexDirection: "column", gap: 10, opacity: 0.6 }}>
            <span style={{ display: "flex", alignItems: "center", gap: 10 }}>
              <ProviderBadge provider="supabase" />
              <ProjectBadge name="acme-dev" />
            </span>
            <span style={{ display: "flex", flexDirection: "column", border: "1px solid var(--line)", borderRadius: "var(--radius-lg)", background: "var(--surface-raised)", overflow: "hidden" }}>
              <span style={exRow}>
                <span className="m-tile red" style={exTile}>
                  <KindIcon kind="table" size={15} />
                </span>
                <span style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
                  <span style={{ font: "500 13.5px var(--font-mono)" }}>customers</span>
                  <span style={exSub}>
                    <SessionAvatar id="8f2a-example" agent="claude-code" size={16} />
                    add customer import
                  </span>
                </span>
                <Badge tone="exposed">Open to anyone</Badge>
              </span>
              <span style={exRow}>
                <span className="m-tile plain" style={exTile}>
                  <KindIcon kind="table" size={15} />
                </span>
                <span style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
                  <span style={{ font: "500 13.5px var(--font-mono)" }}>events</span>
                  <span style={exSub}>
                    <SessionAvatar id="7a0c-example" agent="claude-code" size={16} />
                    track signup events · 8 d
                  </span>
                </span>
                <span className="m-seg" style={{ borderRadius: 7 }}>
                  <span style={{ padding: "4px 8px", fontSize: 11.5, color: "var(--ink-muted)" }}>Keep</span>
                  <span style={{ padding: "4px 8px", fontSize: 11.5, color: "var(--ink-muted)" }}>Not needed</span>
                </span>
              </span>
              <span style={{ ...exRow, borderBottom: 0 }}>
                <span className="m-tile plain" style={exTile}>
                  <KindIcon kind="table" size={15} />
                </span>
                <span style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
                  <span style={{ font: "500 13.5px var(--font-mono)" }}>legacy_users</span>
                  <span style={exSub}>
                    <span style={{ width: 16, height: 16, boxSizing: "border-box", borderRadius: 999, border: "1px dashed var(--control-line)", display: "inline-flex", alignItems: "center", justifyContent: "center" }}>
                      <Icon name="question" size={10} />
                    </span>
                    Origin unknown
                  </span>
                </span>
                <span style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)" }}>private</span>
              </span>
            </span>
            <span style={{ display: "flex", alignItems: "center", gap: 10, marginTop: 6 }}>
              <ProviderBadge provider="vercel" />
              <ProjectBadge name="acme-web" />
            </span>
            <span style={{ display: "flex", flexDirection: "column", border: "1px solid var(--line)", borderRadius: "var(--radius-lg)", background: "var(--surface-raised)", overflow: "hidden" }}>
              <span style={exRow}>
                <span className="m-tile red" style={exTile}>
                  <KindIcon kind="preview" size={15} />
                </span>
                <span style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
                  <span style={{ font: "500 13.5px var(--font-mono)" }}>preview-a81f</span>
                  <span style={exSub}>
                    <SessionAvatar id="8f2a-example" agent="claude-code" size={16} />
                    add customer import
                  </span>
                </span>
                <Badge tone="exposed">Open to anyone</Badge>
              </span>
              <span style={{ ...exRow, borderBottom: 0 }}>
                <span className="m-tile" style={{ ...exTile, background: "transparent", borderColor: "var(--line)", color: "var(--ink-faint)" }}>
                  <KindIcon kind="preview" size={15} />
                </span>
                <span style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
                  <span style={{ font: "500 13.5px var(--font-mono)", textDecoration: "line-through", color: "var(--ink-faint)" }}>preview-1c0e</span>
                  <span style={exSub}>
                    <SessionAvatar id="c2e8-example" agent="codex" size={16} />
                    pricing page previews
                  </span>
                </span>
                <span style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)" }}>gone Oct 25</span>
              </span>
            </span>
          </div>
          <ul style={{ margin: "auto 0 0", padding: "16px 0 0", listStyle: "none", borderTop: "1px solid var(--line)", display: "flex", flexDirection: "column", gap: 12 }}>
            {FACTS.map(([icon, text]) => (
              <li key={text} style={{ display: "flex", alignItems: "center", gap: 10, fontSize: 14, color: "var(--ink-muted)" }}>
                <span style={{ display: "inline-flex", color: "var(--ink-faint)" }}>
                  <Icon name={icon} size={16} />
                </span>
                {text}
              </li>
            ))}
          </ul>
        </section>
      </div>
    </div>
  );
}
