// Project groups and resource rows for My resources. Server components; the
// only interactive piece is the Keep / Not needed toggle.
import Link from "next/link";
import { Badge, Icon } from "@/components/ds";
import { KindIcon, ProjectBadge, ProviderBadge, SessionAvatar, assistantName, providerName } from "@/components/brand";
import { KeepToggle } from "@/components/client";
import { OpenLink } from "@/components/ui";
import { ago, plural, when } from "@/lib/format";
import type { Model, Res } from "@/lib/model";
import { href, ruleShort, rowStatus, shortAge, type Group, type Params } from "./lib";
import s from "./resources.module.css";

export function resourceHref(r: Pick<Res, "id">): string {
  return `/resources/${encodeURIComponent(r.id)}`;
}

function sub(r: Res, m: Model, stale?: Date): string {
  // The project is already in the card header.
  const parts = [r.kind];
  const rule = r.open[0] ? ruleShort(r.open[0].ruleId) : undefined;
  if (rule) parts.push(rule);
  else if (r.existence === "unconfirmed" && r.sessionId) {
    const ses = m.sessions.find((x) => x.id === r.sessionId);
    if (ses) parts.push(`seen only in session ${ses.short}`);
  } else if (stale && r.existence !== "gone" && !r.cantTell.length) parts.push(`as of ${ago(stale, m.now)}`);
  return parts.join(" · ");
}

export function ResourceLine({ r, m, waiting, border = true }: { r: Res; m: Model; waiting: boolean; border?: boolean }) {
  const st = rowStatus(r, waiting);
  const ses = r.sessionId ? m.sessions.find((x) => x.id === r.sessionId) : undefined;
  const born = r.createdAt ?? r.firstSeenAt;
  const stale = r.existence !== "gone" ? m.checks.find((g) => !g.ok && g.provider === r.provider && g.account === r.account)?.lastSuccessAt : undefined;
  const tile = st.tone === "open" ? s.open : st.tone === "dash" ? s.dash : r.existence === "gone" ? s.gone : "";
  return (
    <div className={s.row} style={border ? undefined : { borderBottom: 0 }}>
      <span className={`${s.tile} ${tile}`}>
        <KindIcon kind={r.kind} size={16} />
      </span>
      <span className={s.nameCell}>
        <Link href={resourceHref(r)} className={s.name}>
          <span className={`${s.nameText}${r.existence === "gone" ? ` ${s.gone}` : ""}`}>{r.name}</span>
          <span className={s.sub}>{sub(r, m, stale)}</span>
        </Link>
        <OpenLink r={r} />
      </span>
      {ses ? (
        <Link href={`/sessions/${ses.id}`} className={s.origin} title={`${assistantName(ses.agent)} · session ${ses.short}`}>
          <SessionAvatar id={ses.id} agent={ses.agent} size={26} />
          <span className={s.originText}>
            <span className={s.task}>{ses.title}</span>
            <span className={s.sid}>
              {ses.short} · {when(born, m.now)}
            </span>
          </span>
        </Link>
      ) : (
        <span className={s.origin} title="No recorded session made this">
          <span className={s.unknownDot}>
            <Icon name="question" size={14} />
          </span>
          <span className={s.originText}>
            <span className={s.task} style={{ color: "var(--ink-muted)" }}>
              Origin unknown
            </span>
            <span className={s.sid}>first seen {when(r.firstSeenAt, m.now)}</span>
          </span>
        </span>
      )}
      <span className={s.status}>
        {st.tone === "open" ? <Badge tone="exposed">{st.label}</Badge> : null}
        {st.tone === "wait" ? <Badge tone="review">{st.label}</Badge> : null}
        {st.tone === "dash" ? <span className={s.dash}>{st.label}</span> : null}
        {st.tone === "quiet" && st.label ? (
          <span className={s.quiet} style={st.color ? { color: st.color } : undefined}>
            {st.label}
          </span>
        ) : null}
      </span>
      <span className={s.act}>
        {st.action === "details" ? (
          <Link href={resourceHref(r)} className={s.details}>
            See details →
          </Link>
        ) : st.action === "decide" ? (
          <KeepToggle resourceId={r.id} state="pending" />
        ) : st.action === "link" ? (
          <Link href="/providers" className={s.link}>
            Reconnect {providerName(r.provider)} →
          </Link>
        ) : (
          <span className={s.quiet}>{shortAge(r.ageDays)}</span>
        )}
      </span>
    </div>
  );
}

export function ProjectGroup({ g, m, p, waiting }: { g: Group; m: Model; p: Params; waiting: Set<string> }) {
  const filtered = p.filter !== "all" || !!p.q;
  const count = p.filter === "gone" ? `${g.rows.length} gone` : filtered ? `${g.rows.length} of ${g.total}` : plural(g.total, "resource");
  const expanded = p.expand.includes(g.project.name);
  const canToggle = g.folded ? g.hidden > 0 : expanded && p.filter === "all" && !p.q && !p.project;
  const more = g.folded
    ? `${g.rows.length ? `${g.hidden} more` : plural(g.hidden, "resource")}, none open or waiting`
    : "Show fewer";
  const toggle = href(p, { expand: g.folded ? [...p.expand, g.project.name] : p.expand.filter((x) => x !== g.project.name) });
  return (
    <section aria-label={g.project.name} className={`${s.box}${g.failing ? ` ${s.dashed}` : ""}`}>
      <div className={s.groupHead} style={g.rows.length || canToggle ? undefined : { borderBottom: 0 }}>
        <span className={s.groupTitle}>
          <ProviderBadge provider={g.project.provider} large />
          <ProjectBadge name={g.project.name} large />
          <span className={s.account}>{g.project.account}</span>
        </span>
        {g.open > 0 ? (
          <span className={s.redNote}>
            <span className={s.dot} />
            {g.open} open to anyone
          </span>
        ) : null}
        {g.failing ? <span className={s.dashNote}>can&apos;t tell · last good check {g.failing.lastSuccessAt ? ago(g.failing.lastSuccessAt, m.now) : "never"}</span> : null}
        <span className="m-note-mono" style={{ whiteSpace: "nowrap" }}>
          {count}
        </span>
      </div>
      {g.rows.map((r, i) => (
        <ResourceLine key={r.id} r={r} m={m} waiting={waiting.has(r.id)} border={i < g.rows.length - 1 || canToggle} />
      ))}
      {canToggle ? (
        <Link href={toggle} scroll={false} className={s.more}>
          <span>{more}</span>
          <span style={{ fontSize: 16 }}>{g.folded ? "›" : "‹"}</span>
        </Link>
      ) : null}
    </section>
  );
}
