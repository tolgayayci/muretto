// Shared page pieces. Server components unless noted; the canvas designs are
// the reference for every one of them.
import Link from "next/link";
import type { ReactNode } from "react";
import { Icon, type IconName } from "@/components/ds";
import { KindIcon, ProjectBadge, ProviderBadge, SessionAvatar, providerName } from "@/components/brand";
import { CommandChip } from "@/components/client";
import { ago, clock, when } from "@/lib/format";
import { isConsole, linkText, safeUrl } from "@/lib/links";

/** "14:04" today, otherwise "Oct 29". */
function shortWhen(d: Date, now: Date): string {
  const w = when(d, now);
  return w.startsWith("today ") ? clock(d) : w;
}
import type { Model, Problem, Res, Ses } from "@/lib/model";

export function PageHeader({ eyebrow, title, quiet, right }: { eyebrow: string; title: ReactNode; quiet?: ReactNode; right?: ReactNode }) {
  return (
    <header className="m-head">
      <div className="m-head-text">
        <p className="m-eyebrow">{eyebrow}</p>
        <h1 className="m-title">
          <span>{title}</span>
          {quiet ? <span className="q">{quiet}</span> : null}
        </h1>
      </div>
      {right}
    </header>
  );
}

export function Checked({ m, prefix = "checked" }: { m: Model; prefix?: string }) {
  if (!m.checkedAt) return null;
  return (
    <span className="m-checked">
      <span className="m-dot" />
      {prefix} {ago(m.checkedAt, m.now)}
    </span>
  );
}

export function Section({ title, id, link, note, children }: { title: string; id?: string; link?: { href: string; label: string }; note?: string; children: ReactNode }) {
  return (
    <section className="m-section" aria-labelledby={id}>
      <div className="m-section-head">
        <h2 className="m-h2" id={id}>
          {title}
        </h2>
        {link ? (
          <Link href={link.href} className="m-link-mono">
            {link.label}
          </Link>
        ) : note ? (
          <span className="m-note-mono">{note}</span>
        ) : null}
      </div>
      {children}
    </section>
  );
}

/** One count in a summary strip: number, label, and a one-line explanation. Put them in <nav className="m-card m-strip">. */
export function Count({ label, value, help, icon, tone, href }: { label: string; value: number | string; help: string; icon: IconName; tone?: "red" | "ok"; href: string }) {
  const color = tone === "ok" || (tone === "red" && value !== 0) ? ` ${tone}` : "";
  return (
    <Link href={href} className="m-strip-cell">
      <span className={`m-strip-num${color}`}>{value}</span>
      <span className="m-strip-text">
        <span className="m-strip-label">
          <Icon name={icon} size={14} />
          {label}
        </span>
        <span className="m-strip-help" title={help}>
          {help}
        </span>
      </span>
    </Link>
  );
}

export function StatCard({ label, value, help, icon, tone, href }: { label: string; value: number | string; help: string; icon: IconName; tone?: "red" | "ok"; href: string }) {
  return (
    <Link href={href} className="m-card m-stat">
      <span className="m-stat-top">
        <span className={`m-tile${tone ? ` ${tone}` : ""}`} style={{ width: 32, height: 32 }}>
          {tone === "ok" ? <span style={{ fontSize: 14 }}>✓</span> : <Icon name={icon} size={16} />}
        </span>
        <span className="m-stat-label">{label}</span>
      </span>
      <span className={`m-stat-value${tone === "red" ? " red" : ""}`}>{value}</span>
      <span className="m-stat-help">{help}</span>
    </Link>
  );
}

/** A thing that is open to anyone, as a card. */
type Linkable = { url?: string; provider: string; existence?: string };

function openTarget(r: Linkable): { url: string; label: string; short: string } | null {
  const url = safeUrl(r.url);
  if (!url || r.existence === "gone") return null;
  return isConsole(url)
    ? { url, label: `Open in ${providerName(r.provider)}`, short: `Open in ${providerName(r.provider)}` }
    : { url, label: `Visit ${linkText(url)}`, short: "Visit" };
}

/** A small ↗ next to a resource name: opens the live thing, or its page at the provider, in a new tab. */
export function OpenLink({ r }: { r: Linkable }) {
  const t = openTarget(r);
  if (!t) return null;
  return (
    <a href={t.url} target="_blank" rel="noopener noreferrer" className="m-open" title={t.label} aria-label={t.label}>
      <Icon name="external" size={14} />
    </a>
  );
}

/** The same link as a secondary button, for a resource's own page. */
export function OpenButton({ r }: { r: Linkable }) {
  const t = openTarget(r);
  if (!t) return null;
  return (
    <a href={t.url} target="_blank" rel="noopener noreferrer" className="mu-btn mu-btn-secondary mu-btn-sm" title={t.label}>
      {t.short}
      <Icon name="external" size={14} />
    </a>
  );
}

/** Long names (preview hosts, env vars) stop after two lines. */
const twoLines = { display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical", overflow: "hidden", overflowWrap: "anywhere" } as const;

/** Kind, then the provider and project badges. */
function KindLine({ r }: { r: Res }) {
  return (
    <span style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 6, minWidth: 0 }}>
      <span style={{ fontSize: 12.5, color: "var(--ink-faint)" }}>{r.kind}</span>
      <ProviderBadge provider={r.provider} />
      <ProjectBadge name={r.project} />
    </span>
  );
}

export function IssueCard({ r, m, compact = false }: { r: Res; m: Model; compact?: boolean }) {
  const f = r.open[0];
  const s = r.sessionId ? m.sessions.find((x) => x.id === r.sessionId) : undefined;
  const t = openTarget(r);
  // A name that is the live URL (a preview, a domain) opens it; the card opens the resource page.
  const visit = t && !isConsole(t.url) ? t.url : undefined;
  const name = { font: "500 15px var(--font-mono)", lineHeight: "22px", flexGrow: 1, minWidth: 0, ...twoLines };
  return (
    <div className="m-card m-issue m-stretch-host" style={{ display: "flex", flexDirection: "column", gap: compact ? 14 : 16, padding: compact ? 18 : 20, color: "var(--ink)" }}>
      <span style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
        <span className="m-tile red" style={{ width: 36, height: 36, borderRadius: 10 }}>
          <KindIcon kind={r.kind} size={17} />
        </span>
        <span style={{ display: "flex", flexDirection: "column", gap: 5, flexGrow: 1, minWidth: 0 }}>
          <span style={{ display: "flex", alignItems: "flex-start", gap: 8, minWidth: 0 }}>
            {visit ? (
              <a href={visit} target="_blank" rel="noopener noreferrer" className="m-name-link" title={`Visit ${linkText(visit)}`} style={{ flexGrow: 1, minWidth: 0 }}>
                <span className="t" style={name}>
                  {r.name}
                </span>
                <span className="i">
                  <Icon name="external" size={13} />
                </span>
              </a>
            ) : (
              <span title={r.name} style={name}>
                {r.name}
              </span>
            )}
            {visit ? null : <OpenLink r={r} />}
            <span className="m-sev">{f.severity}</span>
          </span>
          <KindLine r={r} />
        </span>
      </span>
      <span style={{ display: "flex", flexDirection: "column", gap: 6 }}>
        <span style={{ fontSize: compact ? 15 : 16, lineHeight: "22px", fontWeight: 600 }}>{f.what}</span>
        {compact ? null : <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-muted)" }}>{f.why}</span>}
      </span>
      <span style={{ marginTop: "auto", display: "flex", alignItems: "center", gap: 9, paddingTop: 14, borderTop: "1px solid var(--line)" }}>
        {s ? <SessionAvatar id={s.id} agent={s.agent} size={22} /> : null}
        <span style={{ font: "12.5px var(--font-mono)", color: "var(--ink-faint)", whiteSpace: "nowrap" }}>{s ? `${s.short} · ${shortWhen(r.createdAt ?? r.firstSeenAt, m.now)}` : "origin unknown"}</span>
        <Link href={`/resources/${encodeURIComponent(r.id)}`} className="m-stretch m-go" style={{ marginLeft: "auto", fontSize: 13.5, fontWeight: 500, color: "var(--accent)", whiteSpace: "nowrap" }}>
          How to close it →
        </Link>
      </span>
    </div>
  );
}

/** A thing Muretto couldn't check: dashed, never red. */
export function CantTellCard({ r, m }: { r: Res; m: Model }) {
  const last = r.lastSeenAt;
  return (
    <Link href={`/resources/${encodeURIComponent(r.id)}`} className="m-card dashed" style={{ display: "flex", flexDirection: "column", gap: 16, padding: 20, color: "var(--ink)" }}>
      <span style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
        <span className="m-tile dashed" style={{ width: 36, height: 36, borderRadius: 10 }}>
          <KindIcon kind={r.kind} size={17} />
        </span>
        <span style={{ display: "flex", flexDirection: "column", gap: 6, flexGrow: 1, minWidth: 0 }}>
          <span style={{ display: "flex", alignItems: "flex-start", gap: 8, minWidth: 0 }}>
            <span title={r.name} style={{ font: "500 15px var(--font-mono)", lineHeight: "22px", flexGrow: 1, minWidth: 0, ...twoLines }}>
              {r.name}
            </span>
            <span className="m-sev dashed">can&apos;t tell</span>
          </span>
          <KindLine r={r} />
        </span>
      </span>
      <span style={{ display: "flex", flexDirection: "column", gap: 6 }}>
        <span style={{ fontSize: 16, lineHeight: "22px", fontWeight: 600, color: "var(--ink-muted)" }}>Muretto couldn&apos;t check this.</span>
        <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-faint)" }}>The provider check failed, so it might be open to anyone or not.</span>
      </span>
      <span style={{ marginTop: "auto", display: "flex", alignItems: "center", gap: 9, paddingTop: 14, borderTop: "1px dashed var(--line-strong)" }}>
        <span style={{ font: "12.5px var(--font-mono)", color: "var(--ink-faint)" }}>last seen {ago(last, m.now)}</span>
        <span style={{ marginLeft: "auto", fontSize: 13.5, fontWeight: 500 }}>Fix the check →</span>
      </span>
    </Link>
  );
}

const SURFACE_LABEL: Record<string, string> = { "claude-code": "Claude Code", codex: "Codex" };

export function SessionRow({ s, m }: { s: Ses; m: Model }) {
  const made = s.resourceIds.length;
  return (
    <Link href={`/sessions/${s.id}`} className="m-row wrap">
      <SessionAvatar id={s.id} agent={s.agent} size={32} />
      <span className="m-row-main">
        <span className="m-row-title">{s.title}</span>
        <span className="m-row-sub mono" style={{ display: "flex", alignItems: "center", gap: 8 }}>
          {[SURFACE_LABEL[s.agent] ?? s.agent, s.repo].filter(Boolean).join(" · ")}
          {s.transcriptOnly ? <span style={{ padding: "0 6px", border: "1px solid var(--line-strong)", borderRadius: 5, color: "var(--ink-muted)" }}>transcript only</span> : null}
        </span>
      </span>
      <span className="m-row-end">
        {s.openCount > 0 ? (
          <span className="mu-badge mu-badge-exposed">{s.openCount} open to anyone</span>
        ) : (
          <span style={{ font: "12.5px var(--font-mono)", color: "var(--ink-muted)", whiteSpace: "nowrap" }}>{made === 0 ? "nothing made" : `${made} made`}</span>
        )}
        <span className="mono">{s.live ? "running now" : when(s.startedAt, m.now)}</span>
      </span>
    </Link>
  );
}

/** A resource as a row: kind icon, name, sub line, and whatever goes on the right. */
export function ResourceRow({ r, sub, end, href = true }: { r: Res; sub?: ReactNode; end?: ReactNode; href?: boolean }) {
  const tone = r.status === "open" ? " red" : r.status === "cant_tell" || r.status === "unconfirmed" ? " dashed" : " plain";
  const body = (
    <>
      <span className={`m-tile${tone}`} style={{ width: 32, height: 32 }}>
        <KindIcon kind={r.kind} size={16} />
      </span>
      <span className="m-row-main">
        <span className="m-row-name" style={r.status === "gone" ? { textDecoration: "line-through", color: "var(--ink-faint)" } : undefined}>
          {r.name}
        </span>
        <span className="m-row-sub">{sub ?? `${r.kind} · ${r.project}`}</span>
      </span>
      {end}
    </>
  );
  if (!href) return <div className="m-row hover">{body}</div>;
  return (
    <Link href={`/resources/${encodeURIComponent(r.id)}`} className="m-row">
      {body}
    </Link>
  );
}

export function ProblemBox({ problems, title }: { problems: Problem[]; title?: string }) {
  if (!problems.length) return null;
  return (
    <section aria-labelledby="fix-h" className="m-list" style={{ background: "var(--surface-raised)" }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "16px 20px", borderBottom: "1px solid var(--line)" }}>
        <h2 id="fix-h" style={{ margin: 0, fontSize: 17, lineHeight: "22px", fontWeight: 600 }}>
          {title ?? (problems.length === 1 ? "1 thing to fix in your setup" : `${problems.length} things to fix in your setup`)}
        </h2>
        <span className="m-note-mono">until then, some of this page is out of date</span>
      </div>
      {problems.map((p) => (
        <div key={p.title} style={{ display: "flex", alignItems: "center", gap: 14, padding: "16px 20px", borderBottom: "1px solid var(--line)" }}>
          <span className="m-tile" style={{ width: 34, height: 34, color: "var(--ink)" }}>
            <Icon name={p.code === "stale_data" ? "laptop" : p.code.startsWith("hooks") ? "terminal" : "key"} size={16} />
          </span>
          <span style={{ display: "flex", flexDirection: "column", gap: 3, flexGrow: 1, minWidth: 0 }}>
            <span style={{ fontSize: 15, fontWeight: 600 }}>{p.title}</span>
            {p.text ? <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-muted)" }}>{p.text}</span> : null}
          </span>
          {p.command ? <CommandChip command={p.command} small /> : null}
          {p.href ? (
            <Link href={p.href} className="mu-btn mu-btn-secondary mu-btn-sm">
              Open
            </Link>
          ) : null}
        </div>
      ))}
    </section>
  );
}

export function EmptyState({ icon, title, text, children, minHeight = 300 }: { icon: IconName; title: string; text: ReactNode; children?: ReactNode; minHeight?: number }) {
  return (
    <div className="m-empty" style={{ minHeight }}>
      <Icon name={icon} tile />
      <span className="m-empty-title">{title}</span>
      <span className="m-empty-text">{text}</span>
      {children}
    </div>
  );
}

/** Loading placeholder blocks. */
export function Sk({ w, h, dim = false, r }: { w: number | string; h: number; dim?: boolean; r?: number }) {
  return <span className={`m-sk${dim ? " dim" : ""}`} style={{ width: w, height: h, borderRadius: r }} />;
}

export function HeaderSkeleton() {
  return (
    <header style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <Sk w={150} h={14} />
      <Sk w={420} h={34} />
      <Sk w={340} h={34} dim />
    </header>
  );
}

export function RowsSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div className="m-list">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="m-row">
          <Sk w={32} h={32} r={999} />
          <span style={{ display: "flex", flexDirection: "column", gap: 7, flexGrow: 1 }}>
            <Sk w="70%" h={12} />
            <Sk w="45%" h={10} dim />
          </span>
        </div>
      ))}
    </div>
  );
}
