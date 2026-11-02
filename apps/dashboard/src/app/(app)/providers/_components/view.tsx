// Providers page pieces. Server components; the request form is the only client part.
import Link from "next/link";
import type { CSSProperties, ReactNode } from "react";
import { Icon } from "@/components/ds";
import { KindIcon, ProviderLogo, ProviderTile, providerName } from "@/components/brand";
import { CommandChip } from "@/components/client";
import { PageHeader } from "@/components/ui";
import { ago, plural } from "@/lib/format";
import type { CheckGroup, Model } from "@/lib/model";
import { COMING, ENV, FLAGS, LISTS, SUPPORTED, checkProblems, flagResult, groupTone, kindCells, kindLabel, projectCount, type CheckProblem, type Flag, type FlagResult, type KindCell } from "./derive";
import { RequestForm } from "./request-form";

const LABEL: CSSProperties = { font: "500 11.5px var(--font-mono)", letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--ink-faint)" };
const BOX: CSSProperties = { display: "flex", flexDirection: "column", border: "1px solid var(--line)", borderRadius: "var(--radius-lg)", overflow: "hidden" };

type Tone = "ok" | "warn" | "off";

function Pill({ tone, children }: { tone: Tone; children: ReactNode }) {
  const look: CSSProperties =
    tone === "ok"
      ? { border: "1px solid var(--accent-line)", background: "var(--accent-wash)", color: "var(--accent)" }
      : tone === "warn"
        ? { border: "1px dashed var(--control-line)", color: "var(--ink)" }
        : { border: "1px dashed var(--line-strong)", color: "var(--ink-faint)" };
  return (
    <span style={{ flex: "0 0 auto", display: "inline-flex", alignItems: "center", gap: 7, height: 26, padding: "0 10px", borderRadius: 999, whiteSpace: "nowrap", font: "500 12px var(--font-mono)", ...look }}>
      <span style={{ width: 6, height: 6, borderRadius: 999, background: tone === "ok" ? "var(--accent)" : tone === "warn" ? "var(--ink-muted)" : "var(--line-strong)" }} />
      {children}
    </span>
  );
}

function Head({ provider, sub, pill }: { provider: string; sub: string; pill: ReactNode }) {
  return (
    <div className="m-wrap-narrow" style={{ display: "flex", alignItems: "center", gap: 14 }}>
      <ProviderTile provider={provider} size={42} />
      <span style={{ display: "flex", flexDirection: "column", gap: 2, flexGrow: 1, minWidth: 0 }}>
        <span style={{ fontSize: 19, fontWeight: 600, letterSpacing: "-0.01em" }}>{providerName(provider)}</span>
        <span style={{ font: "12.5px var(--font-mono)", color: "var(--ink-faint)", overflowWrap: "anywhere" }}>{sub}</span>
      </span>
      {pill}
    </div>
  );
}

/** What each kind's count means when the check didn't fully work. */
const CELL_NOTE: Record<string, string> = { old: "", retry: "retrying", denied: "no access", failed: "can't check", skipped: "skipped" };

/**
 * Two columns of kind and count. The header carries the check time; a cell
 * says more only when its check didn't work (and its age only when the rest
 * of the account is fresher).
 */
function Kinds({ cells, down = false }: { cells: KindCell[]; down?: boolean }) {
  const rows = Math.ceil(cells.length / 2);
  return (
    <div style={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)", border: "1px solid var(--line)", borderRadius: "var(--radius-lg)", overflow: "hidden", background: "var(--surface)" }}>
      {cells.map((k, i) => {
        const ok = k.state === "ok";
        const alone = i === cells.length - 1 && i % 2 === 0;
        const note = ok ? "" : k.state === "old" ? (down ? "" : k.note) : CELL_NOTE[k.state];
        return (
          <span
            key={k.kind}
            title={`${k.label}: ${k.items ?? "?"}, ${k.note}`}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 10,
              minWidth: 0,
              height: 44,
              padding: "0 14px",
              gridColumn: alone ? "1 / -1" : undefined,
              borderRight: i % 2 === 0 && !alone ? "1px solid var(--line)" : undefined,
              borderBottom: Math.floor(i / 2) < rows - 1 ? "1px solid var(--line)" : undefined,
            }}
          >
            <span style={{ flex: "1 1 auto", minWidth: 0, font: "13px var(--font-mono)", color: "var(--ink-muted)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{k.label}</span>
            {note ? <span style={{ flex: "0 0 auto", padding: "1px 7px", border: "1px dashed var(--control-line)", borderRadius: 6, font: "11.5px var(--font-mono)", color: "var(--ink-muted)", whiteSpace: "nowrap" }}>{note}</span> : null}
            <span style={{ flex: "0 0 auto", minWidth: 16, textAlign: "right", font: "600 15px var(--font-sans)", fontVariantNumeric: "tabular-nums", color: ok ? "var(--ink)" : "var(--ink-faint)" }}>{k.items ?? "–"}</span>
          </span>
        );
      })}
    </div>
  );
}

function FlagRows({ flags, results }: { flags: Flag[]; results?: FlagResult[] }) {
  // Without results the provider isn't connected, so nothing was checked.
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <span style={LABEL}>What it flags</span>
      <div style={BOX}>
        {flags.map((f, i) => {
          const r = results?.[i] ?? { kind: "unknown" as const };
          return (
            <Link key={f.ruleId} href="/resources" className="m-row wrap m-flag-row" style={{ minHeight: 56, gap: 12, paddingLeft: 14, paddingRight: 14, borderBottom: i < flags.length - 1 ? "1px solid var(--line)" : 0 }}>
              <span className="m-tile" style={{ width: 30, height: 30, borderRadius: 8 }}>
                <KindIcon kind={f.icon} size={15} />
              </span>
              <span className="m-row-main" style={{ gap: 2 }}>
                <span className="m-row-title">{f.what}</span>
                <span className="m-row-sub mono">{f.sub}</span>
              </span>
              {r.kind === "open" ? (
                <span className="mu-badge mu-badge-exposed">{r.count} open to anyone</span>
              ) : r.kind === "none" ? (
                <span style={{ flex: "0 0 auto", display: "inline-flex", alignItems: "center", gap: 6, font: "12.5px var(--font-mono)", color: "var(--ink-muted)", whiteSpace: "nowrap" }}>
                  <span style={{ color: "var(--accent)" }}>✓</span>none
                </span>
              ) : (
                <span className="m-sev dashed">{results ? "can't tell" : "not checked"}</span>
              )}
            </Link>
          );
        })}
      </div>
    </div>
  );
}

function cardStyle(dashed: boolean, wide: boolean): CSSProperties {
  return {
    boxSizing: "border-box",
    borderRadius: "var(--radius-2xl)",
    padding: 24,
    display: "flex",
    flexDirection: "column",
    gap: 22,
    gridColumn: wide ? "1 / -1" : undefined,
    border: dashed ? "1px dashed var(--control-line)" : "1px solid var(--line)",
    background: dashed ? "var(--surface)" : "var(--surface-raised)",
  };
}

export function ProviderCard({ m, g, wide = false }: { m: Model; g: CheckGroup; wide?: boolean }) {
  const tone = groupTone(g);
  const cells = kindCells(g, m.now);
  const projects = projectCount(g);
  const attempted = g.checks.map((c) => new Date(c.attempted_at)).filter((d) => !Number.isNaN(d.getTime())).sort((a, b) => b.getTime() - a.getTime())[0];
  const checked =
    tone === "ok"
      ? g.lastSuccessAt
        ? `checked ${ago(g.lastSuccessAt, m.now)}`
        : "checked"
      : tone === "partial"
        ? `partly checked ${attempted ? ago(attempted, m.now) : ""}`.trim()
        : g.lastSuccessAt
          ? `last good check ${ago(g.lastSuccessAt, m.now)}`
          : "never checked";
  const reconnect = tone === "down" && g.failing.some((c) => c.error_code === "auth");
  const flags = FLAGS[g.provider] ?? [];
  const sub = [g.account, projects !== undefined ? plural(projects, "project") : null, tone === "down" && g.lastSuccessAt ? ago(g.lastSuccessAt, m.now) : null].filter(Boolean).join(" · ");
  return (
    <section aria-label={`${providerName(g.provider)} ${g.account}`} style={cardStyle(tone === "down", wide)}>
      <Head provider={g.provider} sub={sub} pill={<Pill tone={tone === "ok" ? "ok" : "warn"}>{tone === "ok" ? "connected" : reconnect ? "reconnect" : "needs a look"}</Pill>} />
      <div style={wide ? { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 400px), 1fr))", gap: 22 } : { display: "contents" }}>
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 12 }}>
            <span style={LABEL}>What it lists</span>
            <span style={{ display: "inline-flex", alignItems: "center", gap: 6, font: "12px var(--font-mono)", color: "var(--ink-faint)", whiteSpace: "nowrap" }}>
              {tone === "ok" ? <span style={{ color: "var(--accent)" }}>✓</span> : null}
              {checked}
            </span>
          </div>
          <Kinds cells={cells} down={tone === "down"} />
        </div>
        {flags.length ? <FlagRows flags={flags} results={flags.map((f) => flagResult(m, g, f))} /> : null}
      </div>
    </section>
  );
}

export function NotConnectedCard({ provider, wide = false }: { provider: string; wide?: boolean }) {
  return (
    <section aria-label={`${providerName(provider)} not connected`} style={cardStyle(true, wide)}>
      <Head provider={provider} sub={`token from ${ENV[provider]}`} pill={<Pill tone="off">not connected</Pill>} />
      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 12 }}>
          <span style={LABEL}>What it lists</span>
          <span style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)" }}>never checked</span>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 12, minHeight: 94, boxSizing: "border-box", justifyContent: "center", padding: 14, border: "1px dashed var(--line-strong)", borderRadius: "var(--radius-lg)", background: "var(--surface)" }}>
          <span style={{ font: "12.5px var(--font-mono)", color: "var(--ink-faint)" }}>
            {(LISTS[provider] ?? []).map((k) => kindLabel(k)).join(" · ")}
          </span>
          <CommandChip command="muretto init" small />
        </div>
      </div>
      <FlagRows flags={FLAGS[provider] ?? []} />
    </section>
  );
}

function ProviderGrid({ m }: { m: Model }) {
  const missing = SUPPORTED.filter((p) => !m.checks.some((g) => g.provider === p));
  const total = m.checks.length + missing.length;
  const odd = total % 2 === 1;
  return (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 520px), 1fr))", gap: 20, alignItems: "stretch" }}>
      {m.checks.map((g, i) => (
        <ProviderCard key={`${g.provider}:${g.accountId}`} m={m} g={g} wide={odd && total > 1 && i === total - 1} />
      ))}
      {missing.map((p, i) => (
        <NotConnectedCard key={p} provider={p} wide={odd && total > 1 && m.checks.length + i === total - 1} />
      ))}
    </div>
  );
}

function ProblemList({ problems, failing }: { problems: CheckProblem[]; failing: number }) {
  return (
    <section aria-labelledby="setup-h" className="m-list" style={{ background: "var(--surface-raised)" }}>
      <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "16px 20px", borderBottom: "1px solid var(--line)" }}>
        <h2 id="setup-h" style={{ margin: 0, fontSize: 17, lineHeight: "22px", fontWeight: 600 }}>
          {failing === 1 ? "1 check didn't work" : `${failing} checks didn't work`}
        </h2>
        <span className="m-note-mono">what they cover shows as can&apos;t tell</span>
      </div>
      {problems.map((p, i) => (
        <div key={p.key} style={{ display: "flex", alignItems: "center", gap: 14, padding: "16px 20px", borderBottom: i < problems.length - 1 ? "1px solid var(--line)" : 0 }}>
          <span className="m-tile" style={{ width: 34, height: 34, color: "var(--ink)" }}>
            <Icon name={p.icon} size={16} />
          </span>
          <span style={{ display: "flex", flexDirection: "column", gap: 3, flexGrow: 1, minWidth: 0 }}>
            <span style={{ fontSize: 15, fontWeight: 600 }}>{p.title}</span>
            <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-muted)" }}>{p.text}</span>
            <span style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)", overflowWrap: "anywhere" }}>{p.code}</span>
          </span>
          {p.command ? (
            <CommandChip command={p.command} small prompt={false} />
          ) : p.retrying ? (
            <span style={{ flex: "0 0 auto", display: "inline-flex", alignItems: "center", gap: 7, height: 30, padding: "0 12px", border: "1px dashed var(--line-strong)", borderRadius: 999, font: "500 12px var(--font-mono)", color: "var(--ink-muted)", whiteSpace: "nowrap" }}>
              <span style={{ width: 6, height: 6, borderRadius: 999, background: "var(--ink-muted)" }} />
              retries by itself
            </span>
          ) : null}
        </div>
      ))}
    </section>
  );
}

function ProviderChip({ provider, soon = false, title }: { provider: string; soon?: boolean; title?: string }) {
  return (
    <span title={title} style={{ display: "inline-flex", alignItems: "center", gap: 6, height: 26, padding: "0 9px 0 4px", border: `1px ${soon ? "dashed var(--line-strong)" : "solid var(--line)"}`, borderRadius: 7, font: "12px var(--font-mono)", color: soon ? "var(--ink-faint)" : "var(--ink-muted)" }}>
      <span style={{ width: 18, height: 18, borderRadius: 5, background: "var(--surface-hover)", display: "inline-flex", alignItems: "center", justifyContent: "center", opacity: soon ? 0.8 : 1 }}>
        <ProviderLogo provider={provider} size={11} />
      </span>
      {providerName(provider)}
    </span>
  );
}

function ReadsToday() {
  return (
    <div className="m-reads" style={{ marginTop: "auto", paddingTop: 14, borderTop: "1px solid var(--line)", fontSize: 13, color: "var(--ink-faint)" }}>
      <span>Today Muretto reads</span>
      <span style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
        {SUPPORTED.map((p) => (
          <ProviderChip key={p} provider={p} />
        ))}
      </span>
      <span>Coming soon</span>
      <span style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
        {COMING.map((c) => (
          <ProviderChip key={c.provider} provider={c.provider} soon title={`${providerName(c.provider)}: ${c.lists}`} />
        ))}
      </span>
    </div>
  );
}

/** "Use something else?": one full-width row, the ask on the left and the form on the right. */
function RequestCard() {
  return (
    <section aria-labelledby="req-h" className="m-card" style={{ borderRadius: "var(--radius-2xl)", padding: "22px 24px", display: "flex", flexDirection: "column", gap: 18 }}>
      <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: "16px 32px" }}>
        <span style={{ flex: "1 1 320px", display: "flex", alignItems: "center", gap: 14, minWidth: 0 }}>
          <span className="m-tile" style={{ width: 42, height: 42, borderRadius: 11, color: "var(--ink)" }}>
            <Icon name="plug" size={19} />
          </span>
          <span style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
            <h2 id="req-h" style={{ margin: 0, fontSize: 17, lineHeight: "22px", fontWeight: 600 }}>
              Use something else?
            </h2>
            <span style={{ fontSize: 14, color: "var(--ink-faint)" }}>Tell us which provider Muretto should read next.</span>
          </span>
        </span>
        <div style={{ flex: "1 1 400px", minWidth: 0 }}>
          <RequestForm />
        </div>
      </div>
      <ReadsToday />
    </section>
  );
}

const READ_ONLY = (
  <span style={{ display: "inline-flex", alignItems: "center", gap: 10, fontSize: 14, color: "var(--ink-muted)" }}>
    <span style={{ display: "inline-flex", color: "var(--ink-faint)" }}>
      <Icon name="lock" size={16} />
    </span>
    Read-only tokens. They never leave your machines.
  </span>
);

export function Connected({ m }: { m: Model }) {
  const problems = checkProblems(m, providerName);
  const failing = m.checks.reduce((n, g) => n + g.failing.length, 0);
  const attempted = m.checks
    .flatMap((g) => g.checks.map((c) => new Date(c.attempted_at)))
    .filter((d) => !Number.isNaN(d.getTime()))
    .sort((a, b) => b.getTime() - a.getTime())[0];
  return (
    <div className="m-page" style={{ gap: 28 }}>
      {failing ? (
        <PageHeader
          eyebrow="Providers · Personal"
          title="Some checks didn't work."
          quiet="Those parts say can't tell, not safe."
          right={
            <span className="m-checked">
              <span className="m-dot off" />
              partly checked {attempted ? ago(attempted, m.now) : ""}
            </span>
          }
        />
      ) : (
        <PageHeader eyebrow="Providers · Personal" title="It can look." quiet="It can't touch." right={READ_ONLY} />
      )}
      {failing ? <ProblemList problems={problems} failing={failing} /> : null}
      <ProviderGrid m={m} />
      <RequestCard />
    </div>
  );
}

export function NothingConnected() {
  const steps = [
    { title: "Install the CLI", note: "Or with Homebrew: brew install tolgayayci/tap/muretto", cmd: "npm i -g muretto" },
    { title: "Connect your providers", note: "Asks for a read-only token for each one. Or set the env vars first.", cmd: "muretto init" },
    { title: "Check what it can see", note: "Lists each check and whether it worked.", cmd: "muretto providers --coverage" },
    { title: "Send results here", note: "Sign in, then turn sync on. Tokens are never sent.", cmd: "muretto login && muretto sync on" },
  ];
  return (
    <div className="m-page" style={{ gap: 32 }}>
      <PageHeader
        eyebrow="Providers · Personal"
        title="Connect Supabase or Vercel."
        quiet="It can look. It can't touch."
        right={
          <span className="m-checked">
            <span className="m-dot off" />
            nothing connected yet
          </span>
        }
      />
      <div className="m-split">
        <section aria-labelledby="steps-h" className="m-card" style={{ borderRadius: "var(--radius-2xl)", padding: 26, display: "flex", flexDirection: "column", gap: 6 }}>
          <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 12, paddingBottom: 10 }}>
            <h2 id="steps-h" style={{ margin: 0, fontSize: 18, lineHeight: "24px", fontWeight: 600 }}>
              Connect from your machine
            </h2>
            <span className="m-note-mono">macOS · Linux</span>
          </div>
          <ol style={{ margin: 0, padding: "0 0 14px", listStyle: "none", display: "flex", flexDirection: "column", flexGrow: 1 }}>
            {steps.map((s, i) => (
              <li key={s.cmd} style={{ flexGrow: 1, display: "grid", gridTemplateColumns: "28px minmax(0, 1fr)", alignContent: "center", gap: 14, padding: "14px 0", borderTop: "1px solid var(--line)" }}>
                <span style={{ width: 26, height: 26, boxSizing: "border-box", borderRadius: 999, border: "1px solid var(--line-strong)", display: "inline-flex", alignItems: "center", justifyContent: "center", font: "600 12px var(--font-mono)", color: "var(--ink-muted)" }}>{i + 1}</span>
                <span style={{ display: "flex", flexDirection: "column", gap: 8, minWidth: 0 }}>
                  <span style={{ display: "flex", flexDirection: "column", gap: 2 }}>
                    <span style={{ fontSize: 15, fontWeight: 600 }}>{s.title}</span>
                    <span style={{ fontSize: 13.5, color: "var(--ink-faint)" }}>{s.note}</span>
                  </span>
                  <CommandChip command={s.cmd} />
                </span>
              </li>
            ))}
          </ol>
          <div style={{ marginTop: "auto", display: "flex", alignItems: "center", gap: 10, padding: "14px 16px", border: "1px dashed var(--line-strong)", borderRadius: "var(--radius-lg)", fontSize: 14, color: "var(--ink-muted)" }}>
            <span className="m-dot off" style={{ width: 8, height: 8, boxShadow: "0 0 0 4px var(--surface-hover)" }} />
            Waiting for your first check. Reload after the first sync.
          </div>
        </section>
        <div style={{ display: "flex", flexDirection: "column", gap: 20, minWidth: 0 }}>
          <section aria-labelledby="sup-h" className="m-card" style={{ flexGrow: 1, borderRadius: "var(--radius-2xl)", background: "var(--surface-sunken)", padding: 24, display: "flex", flexDirection: "column", gap: 16 }}>
            <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 12 }}>
              <h2 id="sup-h" style={{ margin: 0, fontSize: 18, lineHeight: "24px", fontWeight: 600 }}>
                Supported today
              </h2>
              <span className="m-note-mono">read-only</span>
            </div>
            {SUPPORTED.map((p) => (
              <div key={p} className="m-card" style={{ display: "flex", flexDirection: "column", gap: 12, padding: 16, borderRadius: "var(--radius-lg)" }}>
                <span className="m-wrap-narrow" style={{ display: "flex", alignItems: "center", gap: 12 }}>
                  <ProviderTile provider={p} size={36} />
                  <span style={{ display: "flex", flexDirection: "column", gap: 2, flexGrow: 1, minWidth: 0 }}>
                    <span style={{ fontSize: 16, fontWeight: 600 }}>{providerName(p)}</span>
                    <span style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)", overflowWrap: "anywhere" }}>{ENV[p]}</span>
                  </span>
                  <Pill tone="off">not connected</Pill>
                </span>
                <span style={{ font: "12px var(--font-mono)", color: "var(--ink-muted)" }}>{(LISTS[p] ?? []).map((k) => kindLabel(k)).join(" · ")}</span>
                <span style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                  {(FLAGS[p] ?? []).map((f) => (
                    <span key={f.ruleId} style={{ display: "inline-flex", alignItems: "center", gap: 7, height: 28, padding: "0 10px", border: "1px solid var(--line)", borderRadius: 8, fontSize: 13, color: "var(--ink-muted)" }}>
                      <span className="m-dot red" />
                      {f.short}
                    </span>
                  ))}
                </span>
              </div>
            ))}
            <span style={{ marginTop: "auto", display: "flex", alignItems: "center", gap: 10, fontSize: 13.5, color: "var(--ink-faint)" }}>
              <span style={{ display: "inline-flex" }}>
                <Icon name="lock" size={16} />
              </span>
              Tokens stay on your machine. They are never synced.
            </span>
          </section>
          <RequestCard />
        </div>
      </div>
    </div>
  );
}
