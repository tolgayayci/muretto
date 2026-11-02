import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import type { ReactNode } from "react";
import { Badge, Icon } from "@/components/ds";
import { ProjectBadge, ProviderBadge, SessionAvatar, assistantName, providerName } from "@/components/brand";
import { OpenButton } from "@/components/ui";
import { loadModel } from "@/lib/data";
import { age, ago, clock, day, when } from "@/lib/format";
import { linkText, safeUrl } from "@/lib/links";
import { resourceById, type Finding, type Model, type Res } from "@/lib/model";
import { CopyButton, DecideButtons } from "../_components/client";
import { failingGroup } from "../_components/lib";
import s from "../_components/resources.module.css";

type Props = { params: Promise<{ id: string }> };

function decodeId(raw: string): string {
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

async function load(params: Props["params"]): Promise<{ m: Model; r: Res | undefined }> {
  const [m, { id }] = await Promise.all([loadModel(), params]);
  if (typeof id !== "string" || id.length > 1024) return { m, r: undefined };
  return { m, r: resourceById(m, id) ?? resourceById(m, decodeId(id)) };
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { r } = await load(params);
  return { title: r ? r.name : "Resource not found" };
}

const an = (w: string) => (/^[aeiou]/i.test(w) ? `an ${w}` : `a ${w}`);
const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

const OPEN_TITLE: Record<string, string> = {
  rls_disabled: "A table anyone can read.",
  public_bucket: "A bucket anyone can download from.",
  public_preview: "A preview anyone can open.",
  client_exposed_env: "A key every browser can read.",
};

function headline(r: Res, waiting: boolean): string {
  if (r.existence === "gone") return `This ${r.kind} is gone.`;
  if (r.open.length) return OPEN_TITLE[r.open[0].ruleId] ?? `This ${r.kind} is open to anyone.`;
  if (r.cantTell.length) return `Muretto can't tell if this ${r.kind} is open.`;
  if (r.existence === "unconfirmed") return "Seen in a session, not at the provider.";
  if (waiting) return `Still need this ${r.kind}?`;
  if (r.findings.some((f) => f.state === "not_exposed")) return "Not open to anyone.";
  return `${cap(an(r.kind))} in ${r.project}.`;
}

function originLine(r: Res): string {
  if (r.origin === "unknown") return "Origin unknown: no recorded session made it.";
  if (r.ageDays <= 0) return "Your agent made it today.";
  if (r.ageDays === 1) return "Your agent made it yesterday.";
  return `Your agent made it ${age(r.ageDays)} ago.`;
}

function reviewLine(r: Res): string {
  const rv = r.review;
  if (!rv) return "";
  if (rv.state === "keep") return `kept${rv.decidedAt ? ` · ${day(rv.decidedAt)}` : ""}`;
  if (rv.state === "not_needed") return `not needed${rv.decidedAt ? ` · ${day(rv.decidedAt)}` : ""}`;
  if (rv.overdueDays < 0) return `review due in ${age(-rv.overdueDays)}`;
  if (rv.overdueDays === 0) return "review due today";
  return `waiting on you since ${day(rv.dueAt)}`;
}

const card = { border: "1px solid var(--line)", borderRadius: "var(--radius-2xl)", background: "var(--surface-raised)", padding: 26, display: "flex", flexDirection: "column", gap: 18 } as const;
const side = { ...card, padding: 22, gap: 0 } as const;
const h2 = { margin: 0, fontSize: 18, lineHeight: "24px", fontWeight: 600 } as const;
const h2s = { margin: 0, fontSize: 16, lineHeight: "22px", fontWeight: 600 } as const;
const body = { margin: 0, fontSize: 15, lineHeight: "23px", color: "var(--ink-muted)" } as const;
const foot = { display: "flex", flexWrap: "wrap", justifyContent: "space-between", gap: 8, paddingTop: 14, borderTop: "1px solid var(--line)", font: "12.5px var(--font-mono)", color: "var(--ink-faint)" } as const;

export default async function ResourcePage({ params }: Props) {
  const { m, r } = await load(params);
  if (!r) notFound();
  const waiting = m.waiting.some((x) => x.id === r.id);
  const gone = r.existence === "gone";
  const showReview = !!r.review && r.origin === "agent" && !gone;

  return (
    <div className="m-page" style={{ gap: 32, marginTop: -8 }}>
      <nav aria-label="Breadcrumb" style={{ font: "13px var(--font-mono)", color: "var(--ink-faint)", minWidth: 0, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
        <Link href="/resources" style={{ color: "var(--ink-muted)" }}>
          My resources
        </Link>{" "}
        / {r.name}
      </nav>
      <header className="m-head">
        <div className="m-head-text">
          <p className="m-eyebrow">
            {r.kind} · {r.name}
          </p>
          <h1 className="m-title">
            <span>{headline(r, waiting)}</span>
            <span className="q">{gone ? `Not at the provider since ${day(r.goneAt ?? r.lastSeenAt)}.` : originLine(r)}</span>
          </h1>
        </div>
        <span style={{ display: "inline-flex", flexWrap: "wrap", alignItems: "center", gap: 10 }}>
          <OpenButton r={r} />
          <StatusMark r={r} waiting={waiting} />
        </span>
      </header>

      <div className={s.detail}>
        <div style={{ display: "flex", flexDirection: "column", gap: 24, minWidth: 0 }}>
          {gone ? null : r.open.map((f) => <FindingCard key={f.id} f={f} r={r} m={m} />)}
          {gone ? null : r.cantTell.map((f) => <CantTellCard key={f.id} f={f} r={r} m={m} />)}
          {gone || r.open.length || r.cantTell.length ? null : <StatusCard r={r} m={m} />}
          {gone ? <StatusCard r={r} m={m} /> : null}
          <WhoMadeIt r={r} m={m} />
        </div>
        <aside style={{ display: "flex", flexDirection: "column", gap: 24, minWidth: 0 }}>
          {showReview && r.review ? (
            <section aria-labelledby="r-h" style={side}>
              <h2 id="r-h" style={h2s}>
                Still need it?
              </h2>
              <DecideButtons resourceId={r.id} state={r.review.state === "pending" ? "pending" : r.review.state} line={reviewLine(r)} />
              <span style={{ marginTop: 16, fontSize: 13.5, lineHeight: "20px", color: "var(--ink-faint)" }}>Only you see this. Muretto never deletes anything.</span>
            </section>
          ) : null}
          <Details r={r} m={m} />
        </aside>
      </div>
    </div>
  );
}

function StatusMark({ r, waiting }: { r: Res; waiting: boolean }) {
  if (r.existence === "gone") return <span className="m-sev">gone</span>;
  if (r.open.length) {
    const f = r.open[0];
    return (
      <>
        <span style={{ font: "500 12.5px var(--font-mono)", color: "var(--ink-muted)" }}>{f.severity}</span>
        <Badge tone="exposed">Open to anyone</Badge>
      </>
    );
  }
  if (r.cantTell.length) return <span className="m-sev dashed">can&apos;t tell</span>;
  if (waiting) return <Badge tone="review" />;
  if (r.origin === "unknown") return <Badge tone="unknown" />;
  return null;
}

function FindingCard({ f, r, m }: { f: Finding; r: Res; m: Model }) {
  const text = f.explanation && f.explanation !== f.title ? f.explanation : `${f.what} ${f.why}`;
  const code = f.fix.kind === "sql" || f.fix.kind === "command";
  const docs = safeUrl(f.fix.docs_url);
  return (
    <section aria-labelledby={`f-${f.id}`} style={card}>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <h2 id={`f-${f.id}`} style={h2}>
          {f.title}
        </h2>
        <p style={body}>{text}</p>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, minHeight: 30 }}>
          <span style={{ fontSize: 14, fontWeight: 500 }}>How to close it</span>
          {code ? <CopyButton text={f.fix.text} label={f.fix.kind === "sql" ? "Copy SQL" : "Copy"} /> : null}
        </div>
        {code ? (
          <code style={{ display: "block", padding: "14px 16px", border: "1px solid var(--line)", borderRadius: "var(--radius-md)", background: "var(--surface-sunken)", font: "13.5px/1.6 var(--font-mono)", color: "var(--ink)", whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{f.fix.text}</code>
        ) : (
          <p style={{ margin: 0, padding: "14px 16px", border: "1px solid var(--line)", borderRadius: "var(--radius-md)", background: "var(--surface-sunken)", fontSize: 14.5, lineHeight: "22px", color: "var(--ink)" }}>{f.fix.text}</p>
        )}
        <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-faint)" }}>
          {f.fix.note ? `${f.fix.note} ` : ""}
          {code ? "Muretto never runs this for you." : "Muretto never changes this for you."}
        </span>
        {f.caveat ? <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-faint)" }}>Muretto can&apos;t see everything here: {f.caveat}</span> : null}
      </div>
      <div style={foot}>
        <span>{f.checkedAt ? `checked ${ago(f.checkedAt, m.now)}` : "not checked yet"}</span>
        {docs ? (
          <a href={docs} target="_blank" rel="noopener noreferrer" style={{ color: "var(--ink-muted)" }}>
            {providerName(r.provider)} docs →
          </a>
        ) : null}
      </div>
    </section>
  );
}

function CantTellCard({ f, r, m }: { f: Finding; r: Res; m: Model }) {
  const g = failingGroup(m, r);
  return (
    <section aria-labelledby={`c-${f.id}`} style={{ ...card, border: "1px dashed var(--control-line)", background: "var(--surface)" }}>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <h2 id={`c-${f.id}`} style={{ ...h2, color: "var(--ink-muted)" }}>
          Muretto couldn&apos;t check this.
        </h2>
        <p style={body}>
          The {providerName(r.provider)} check failed, so it might be open to anyone or not. What Muretto looks for: {f.what.charAt(0).toLowerCase() + f.what.slice(1)}
        </p>
      </div>
      <div style={{ ...foot, borderTop: "1px dashed var(--line-strong)" }}>
        <span>{g?.lastSuccessAt ? `last good check ${ago(g.lastSuccessAt, m.now)}` : "never checked"}</span>
        <Link href="/providers" style={{ color: "var(--ink)", fontFamily: "var(--font-sans)", fontSize: 13.5, fontWeight: 500 }}>
          Reconnect {providerName(r.provider)} →
        </Link>
      </div>
    </section>
  );
}

function StatusCard({ r, m }: { r: Res; m: Model }) {
  let icon: ReactNode = <Icon name="check" size={16} />;
  let tone = "m-tile ok";
  let title: string;
  let text: string;
  const checked = r.findings.filter((f) => f.state === "not_exposed");
  if (r.existence === "gone") {
    tone = "m-tile";
    icon = <Icon name="x" size={16} />;
    title = `Gone from ${providerName(r.provider)}.`;
    text = `Muretto last saw it ${ago(r.lastSeenAt, m.now)}. Nothing to close or decide.`;
  } else if (r.existence === "unconfirmed") {
    tone = "m-tile dashed";
    icon = <Icon name="question" size={16} />;
    title = `Not in ${providerName(r.provider)}'s list yet.`;
    text = "It's in the agent's own calls, but the provider doesn't list it. The call may have failed, or it was deleted already.";
  } else if (checked.length) {
    const last = checked.map((f) => f.checkedAt).filter((d): d is Date => !!d).sort((a, b) => b.getTime() - a.getTime())[0];
    title = "Not open to anyone.";
    text = `Muretto checked it${last ? ` ${ago(last, m.now)}` : ""}. It only reads; nothing was changed.`;
  } else {
    tone = "m-tile";
    icon = <Icon name="shield" size={16} />;
    title = `Nothing to check on ${an(r.kind)}.`;
    text = "Muretto checks tables, buckets, previews and browser env vars for things open to anyone.";
  }
  return (
    <section aria-label="Status" style={{ ...card, flexDirection: "row", alignItems: "center", gap: 14, padding: "20px 22px" }}>
      <span className={tone} style={{ width: 36, height: 36, borderRadius: 10 }}>
        {icon}
      </span>
      <span style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
        <span style={{ fontSize: 16, fontWeight: 600 }}>{title}</span>
        <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-muted)" }}>{text}</span>
      </span>
    </section>
  );
}

const shortTool = (t: string) => t.split("__").pop() || t;
const toolWord = (t: string) => (/migration|sql/i.test(t) ? "SQL" : t === "Bash" ? "command" : "call");

function WhoMadeIt({ r, m }: { r: Res; m: Model }) {
  const ses = r.sessionId ? m.sessions.find((x) => x.id === r.sessionId) : undefined;
  const evidence = r.evidence.filter((e) => e.excerpt).slice(0, 3);
  return (
    <section aria-labelledby="w-h" style={{ ...card, gap: 16 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12 }}>
        <h2 id="w-h" style={h2}>
          Who made it
        </h2>
        {ses ? (
          <span style={{ display: "inline-flex", alignItems: "center", gap: 6, font: "12.5px var(--font-mono)", color: "var(--ink-muted)" }}>
            <span style={{ color: "var(--accent)" }}>✓</span>exact
          </span>
        ) : null}
      </div>
      {ses ? (
        <Link href={`/sessions/${ses.id}`} className="m-row" style={{ minHeight: 0, gap: 12, padding: 12, margin: "0 -12px", border: 0, borderRadius: "var(--radius-lg)" }}>
          <SessionAvatar id={ses.id} agent={ses.agent} size={34} />
          <span style={{ display: "flex", flexDirection: "column", gap: 2, flexGrow: 1, minWidth: 0 }}>
            <span style={{ fontSize: 15, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{ses.title}</span>
            <span style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
              {[ses.short, assistantName(ses.agent), ses.repo, when(ses.startedAt, m.now)].filter(Boolean).join(" · ")}
            </span>
          </span>
          <span style={{ fontSize: 16, color: "var(--ink-faint)" }}>›</span>
        </Link>
      ) : (
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <span style={{ flex: "0 0 34px", height: 34, boxSizing: "border-box", borderRadius: 999, border: "1px dashed var(--control-line)", display: "inline-flex", alignItems: "center", justifyContent: "center", color: "var(--ink-faint)" }}>
            <Icon name="question" size={16} />
          </span>
          <span style={{ display: "flex", flexDirection: "column", gap: 2, minWidth: 0 }}>
            <span style={{ fontSize: 15, color: "var(--ink-muted)" }}>Origin unknown</span>
            <span style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)" }}>
              {r.originReason} · first seen {when(r.firstSeenAt, m.now)}
            </span>
          </span>
        </div>
      )}
      {ses && evidence.length ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          <span style={{ fontSize: 14, color: "var(--ink-muted)" }}>
            The {r.kind} name is in the agent&apos;s own {toolWord(evidence[0].tool_name)}:
          </span>
          {evidence.map((e) => {
            const at = new Date(e.occurred_at);
            return (
              <code key={e.action_id} title={e.excerpt} style={{ display: "flex", gap: 14, padding: "12px 14px", border: "1px solid var(--line)", borderRadius: "var(--radius-md)", background: "var(--surface-sunken)", font: "12.5px var(--font-mono)", color: "var(--ink-muted)", whiteSpace: "nowrap", overflow: "hidden" }}>
                {Number.isNaN(at.getTime()) ? null : <span style={{ color: "var(--ink-faint)" }}>{clock(at)}</span>}
                <span style={{ color: "var(--ink)" }}>{shortTool(e.tool_name)}</span>
                <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{e.excerpt}</span>
              </code>
            );
          })}
        </div>
      ) : null}
      {ses ? (
        <div style={{ ...foot, marginTop: "auto" }}>
          <span>how Muretto knows: {r.originReason}</span>
          <Link href={`/sessions/${ses.id}`} style={{ color: "var(--ink-muted)" }}>
            Open session →
          </Link>
        </div>
      ) : (
        <span style={{ marginTop: "auto", paddingTop: 14, borderTop: "1px solid var(--line)", fontSize: 14, lineHeight: "21px", color: "var(--ink-faint)" }}>
          It was already in your account, or made outside a recorded session. Muretto still checks whether it&apos;s open to anyone, but never puts it up for review.
        </span>
      )}
    </section>
  );
}

function Details({ r, m }: { r: Res; m: Model }) {
  const url = safeUrl(r.url);
  const rows: [string, string][] = [
    ["account", r.account],
    ["name", r.name],
    ...(r.createdAt ? ([["created", when(r.createdAt, m.now)]] as [string, string][]) : []),
    ["first seen", when(r.firstSeenAt, m.now)],
    r.existence === "gone" ? ["gone", r.goneAt ? when(r.goneAt, m.now) : "yes"] : r.existence === "unconfirmed" ? ["at the provider", "not listed"] : ["last seen", ago(r.lastSeenAt, m.now)],
  ];
  return (
    <section aria-labelledby="d-h" style={side}>
      <h2 id="d-h" style={{ ...h2s, marginBottom: 12 }}>
        Details
      </h2>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 6, paddingBottom: 12, borderBottom: "1px solid var(--line)" }}>
        <ProviderBadge provider={r.provider} />
        <ProjectBadge name={r.project} />
      </div>
      {rows.map(([k, v]) => (
        <div key={k} style={{ display: "flex", justifyContent: "space-between", gap: 12, padding: "9px 0", borderBottom: "1px solid var(--line)", font: "12.5px var(--font-mono)" }}>
          <span style={{ color: "var(--ink-faint)", flex: "0 0 auto" }}>{k}</span>
          <span title={v} style={{ color: "var(--ink)", textAlign: "right", minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            {v}
          </span>
        </div>
      ))}
      {url && r.existence !== "gone" ? (
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 12, padding: "9px 0", borderBottom: "1px solid var(--line)", font: "12.5px var(--font-mono)" }}>
          <span style={{ color: "var(--ink-faint)", flex: "0 0 auto" }}>link</span>
          <a href={url} target="_blank" rel="noopener noreferrer" title={url} style={{ display: "inline-flex", alignItems: "center", gap: 6, minWidth: 0, color: "var(--accent)" }}>
            <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{linkText(url)}</span>
            <Icon name="external" size={13} />
          </a>
        </div>
      ) : null}
      <span style={{ marginTop: "auto", paddingTop: 16, display: "flex", alignItems: "center", gap: 8, fontSize: 13.5, color: "var(--ink-faint)" }}>
        <Icon name="lock" size={14} /> Muretto only reads this. Changes happen in {providerName(r.provider)}.
      </span>
    </section>
  );
}
