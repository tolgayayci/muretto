// Session page sections. Server components; the only client pieces are the
// copy button, Keep / Not needed, and native <details> for folding.
import Link from "next/link";
import type { CSSProperties, ReactNode } from "react";
import { Badge, Icon } from "@/components/ds";
import { AssistantGlyph, KindIcon, ProjectBadge, ProviderBadge, SessionAvatar, assistantName, providerName } from "@/components/brand";
import { CantTellCard, Count, OpenLink, Section } from "@/components/ui";
import { CommandChip, KeepToggle } from "@/components/client";
import { ago, age, clock, day, duration, plural, severitySummary, word } from "@/lib/format";
import type { Model, Res } from "@/lib/model";
import { CopyButton } from "./copy-button";
import { creatingCalls, groupByProject, joinAnd, kindSummary, madeBy, notCheckedYet, rowSub, shortStamp, stamp, type Facts } from "./derive";
import css from "./session.module.css";

const mono = (size = 12.5, color = "var(--ink-faint)") => ({ font: `${size}px var(--font-mono)`, color });
const sameDay = (a: Date, b: Date) => day(a) === day(b);

// ------------------------------------------------------------------ header

function quietLine(f: Facts): string {
  const { s, rs, open, cantTell, waiting, gone, unchecked } = f;
  if (s.live) {
    if (open.length) return `Still working. ${plural(open.length, "thing")} ${open.length === 1 ? "is" : "are"} open to anyone so far.`;
    return rs.length ? `Still working. It has made ${plural(rs.length, "thing")} so far.` : "Still working. Nothing made so far.";
  }
  if (!rs.length) return "Nothing was left behind.";
  if (open.length) return `It left ${plural(open.length, "thing")} open to anyone.`;
  if (s.transcriptOnly) return `It made ${plural(rs.length, "thing")}. There may be more.`;
  if (gone.length) {
    const left = rs.length - gone.length;
    const g = rs.length === 1 ? "It's gone now." : `${word(gone.length).replace(/^./, (c) => c.toUpperCase())} ${gone.length === 1 ? "is" : "are"} gone.`;
    if (waiting.length) return `${g} ${word(waiting.length).replace(/^./, (c) => c.toUpperCase())} ${waiting.length === 1 ? "is" : "are"} waiting on you.`;
    return left ? `${g} ${word(left).replace(/^./, (c) => c.toUpperCase())} still there, none open.` : g;
  }
  if (cantTell.length) return `Muretto can't check ${cantTell.length === rs.length ? (rs.length === 1 ? "it" : "them") : `${cantTell.length} of them`}.`;
  if (unchecked.length) return "Muretto hasn't checked what it made yet.";
  return "Nothing it made is open to anyone.";
}

export function Header({ m, f }: { m: Model; f: Facts }) {
  const s = f.s;
  const eyebrow = ["Agent session", s.short, s.live ? "live" : null, s.transcriptOnly ? "transcript only" : null].filter(Boolean).join(" · ");
  const end = s.endedAt ?? s.lastActivityAt;
  const range = s.live
    ? `${stamp(s.startedAt, m.now)} → now · ${duration(s.durationMin)}`
    : end
      ? `${stamp(s.startedAt, m.now)} → ${sameDay(s.startedAt, end) ? clock(end) : shortStamp(end, m.now)} · ${duration(s.durationMin)}`
      : stamp(s.startedAt, m.now);
  const chips: { icon: ReactNode; text: string }[] = [
    { icon: <AssistantGlyph agent={s.agent} size={13} />, text: [assistantName(s.agent), s.agentVersion].filter(Boolean).join(" ") },
    ...(s.model ? [{ icon: <Icon name="sparkle" size={15} />, text: s.model }] : []),
    ...(s.repo ? [{ icon: <Icon name="code" size={15} />, text: s.repo }] : []),
    ...(s.branch ? [{ icon: <KindIcon kind="branch" size={15} />, text: s.branch }] : []),
    { icon: <Icon name="clock" size={15} />, text: range },
  ];
  const checked = m.checkedAt ? `checked ${ago(m.checkedAt, m.now)}` : null;
  const status = s.live
    ? { text: `running now${s.lastActivityAt ? ` · active ${ago(s.lastActivityAt, m.now)}` : ""}`, tone: "live" as const }
    : s.transcriptOnly
      ? { text: ["read from the transcript", checked].filter(Boolean).join(" · "), tone: "muted" as const }
      : { text: [end ? `ended ${shortStamp(end, m.now)}` : null, checked].filter(Boolean).join(" · "), tone: "ok" as const };
  return (
    <header style={{ display: "flex", flexDirection: "column", gap: 22 }}>
      <div className={css.head}>
        <SessionAvatar id={s.id} agent={s.agent} size={56} />
        <div style={{ display: "flex", flexDirection: "column", gap: 10, minWidth: 0, flexGrow: 1 }}>
          <p className="m-eyebrow">{eyebrow}</p>
          <h1 className="m-title">
            <span>“{s.title}”</span>
            <span className="q">{quietLine(f)}</span>
          </h1>
        </div>
        <span style={{ flex: "0 0 auto", display: "inline-flex", alignItems: "center", gap: 8, marginTop: 4, ...mono(12.5, status.tone === "live" ? "var(--accent)" : "var(--ink-faint)") }}>
          {status.tone === "live" ? <span className={css.ping} /> : <span className={`m-dot${status.tone === "muted" ? " off" : ""}`} />}
          {status.text}
        </span>
      </div>
      <div className={css.chips}>
        {chips.map((c) => (
          <span key={c.text} className="m-chip">
            <span style={{ display: "inline-flex", color: "var(--ink-faint)" }}>{c.icon}</span>
            {c.text}
          </span>
        ))}
      </div>
    </header>
  );
}

// ------------------------------------------------------------------- stats

export function Stats({ m, f }: { m: Model; f: Facts }) {
  const { s, rs, open, cantTell, waiting, gone, pending, unchecked } = f;
  const still = rs.length - gone.length;
  const providers = [...new Set(rs.map((r) => providerName(r.provider)))];
  const nextDue = pending.map((r) => r.review!.dueAt).sort((a, b) => a.getTime() - b.getTime())[0];
  const decided = rs.filter((r) => r.review && r.review.state !== "pending").length;
  const checked = s.transcriptOnly ? "of what was found" : !m.checkedAt ? "not checked yet" : unchecked.length ? `${plural(unchecked.length, "new thing")} checked next` : `checked ${ago(m.checkedAt, m.now)}`;
  return (
    <nav aria-label="Summary" className="m-card m-strip">
      {open.length ? (
        <Count label="Open to anyone" value={open.length} help={s.live ? "so far" : severitySummary(open.map((r) => r.open[0].severity))} icon="globe" tone="red" href="#open-h" />
      ) : cantTell.length ? (
        <Count label="Can't tell" value={cantTell.length} help="couldn't be checked" icon="question" href="#open-h" />
      ) : (
        <Count label="Open to anyone" value={0} help={checked} icon="globe" tone="ok" href="#made-h" />
      )}
      <Count
        label={s.live ? "Made so far" : "Made by this session"}
        value={rs.length}
        help={s.transcriptOnly ? "from the transcript, maybe more" : gone.length ? `${still} still there, ${gone.length} gone` : rs.length === 1 ? `${rs[0].name}, still there` : "all still there"}
        icon="sparkle"
        href="#made-h"
      />
      {gone.length ? (
        <Count label="Gone" value={gone.length} help={gone.length === 1 && gone[0].goneAt ? `removed ${day(gone[0].goneAt)}` : "removed since"} icon="x" href="#made-h" />
      ) : (
        <Count label={providers.length === 1 ? "Provider" : "Providers"} value={providers.length} help={joinAnd(providers)} icon="key" href="#made-h" />
      )}
      {s.live ? (
        <Count label="Running for" value={duration(s.durationMin)} help={`since ${clock(s.startedAt)}`} icon="clock" href="#did-h" />
      ) : waiting.length ? (
        <Count label="Waiting on you" value={waiting.length} help="made over 7 days ago" icon="clock" href="/resources?filter=waiting" />
      ) : nextDue ? (
        <Count label="Still needed?" value={day(nextDue)} help="you'll be asked then" icon="clock" href="#made-h" />
      ) : (
        <Count label="Still needed?" value={decided ? "Done" : "—"} help={decided ? "you decided" : "nothing to decide"} icon="check-circle" href="#made-h" />
      )}
    </nav>
  );
}

// ---------------------------------------------------------- open to anyone

function FixCard({ r, m }: { r: Res; m: Model }) {
  const f = r.open[0];
  const by = madeBy(r);
  const code = f.fix.kind !== "steps";
  return (
    <div className="m-card" style={{ display: "flex", flexDirection: "column", gap: 16, padding: 20 }}>
      <span style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
        <span className="m-tile red" style={{ width: 36, height: 36, borderRadius: 10 }}>
          <KindIcon kind={r.kind} size={17} />
        </span>
        <span style={{ display: "flex", flexDirection: "column", gap: 6, flexGrow: 1, minWidth: 0 }}>
          <span style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>
            <span style={{ font: "500 15px var(--font-mono)", lineHeight: "22px", overflowWrap: "anywhere", flexGrow: 1, minWidth: 0 }}>{r.name}</span>
            <span className="m-sev">{f.severity}</span>
          </span>
          <span style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 6 }}>
            <span style={{ fontSize: 12.5, color: "var(--ink-faint)" }}>{r.kind}</span>
            <ProviderBadge provider={r.provider} />
            <ProjectBadge name={r.project} />
          </span>
        </span>
      </span>
      <span style={{ display: "flex", flexDirection: "column", gap: 6 }}>
        <span style={{ fontSize: 16, lineHeight: "22px", fontWeight: 600 }}>{f.what}</span>
        <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-muted)" }}>{f.why}</span>
      </span>
      <div style={{ marginTop: "auto", display: "flex", flexDirection: "column", gap: 10, padding: 14, border: "1px solid var(--line)", borderRadius: "var(--radius-lg)", background: "var(--surface-sunken)" }}>
        <span style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 10, minHeight: 28 }}>
          <span style={{ font: "500 12.5px var(--font-mono)", color: "var(--accent)" }}>How to close it</span>
          {code ? <CopyButton text={f.fix.text} /> : null}
        </span>
        {code ? (
          <code style={{ font: "12.5px/19px var(--font-mono)", color: "var(--ink)", whiteSpace: "pre-wrap", wordBreak: "break-word" }}>{f.fix.text}</code>
        ) : (
          <span style={{ fontSize: 13.5, lineHeight: "20px", color: "var(--ink)" }}>{f.fix.text}</span>
        )}
        {f.fix.note ? <span style={{ fontSize: 12.5, lineHeight: "18px", color: "var(--ink-faint)" }}>{f.fix.note}</span> : null}
      </div>
      <span style={{ display: "flex", alignItems: "center", gap: 9, paddingTop: 14, borderTop: "1px solid var(--line)" }}>
        <span style={{ ...mono(), overflowWrap: "anywhere", minWidth: 0 }}>{by ? `made ${shortStamp(by.at, m.now)} · ${by.short}` : `made ${shortStamp(r.createdAt ?? r.firstSeenAt, m.now)}`}</span>
        <Link href={`/resources/${encodeURIComponent(r.id)}`} style={{ marginLeft: "auto", fontSize: 13.5, fontWeight: 500, color: "var(--ink-muted)", whiteSpace: "nowrap" }}>
          Details →
        </Link>
      </span>
    </div>
  );
}

/** Live sessions: made after the last check, so Muretto hasn't looked yet. */
function NotCheckedCard({ r, m }: { r: Res; m: Model }) {
  const by = madeBy(r);
  return (
    <Link href={`/resources/${encodeURIComponent(r.id)}`} className="m-card dashed" style={{ display: "flex", flexDirection: "column", gap: 16, padding: 20, color: "var(--ink)" }}>
      <span style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
        <span className="m-tile dashed" style={{ width: 36, height: 36, borderRadius: 10 }}>
          <KindIcon kind={r.kind} size={17} />
        </span>
        <span style={{ display: "flex", flexDirection: "column", gap: 6, flexGrow: 1, minWidth: 0 }}>
          <span style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <span style={{ font: "500 15px var(--font-mono)", lineHeight: "22px", flexGrow: 1, minWidth: 0, overflowWrap: "anywhere" }}>{r.name}</span>
            <span className="m-sev dashed">can&apos;t tell yet</span>
          </span>
          <span style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 6 }}>
            <span style={{ fontSize: 12.5, color: "var(--ink-faint)" }}>{r.kind}</span>
            <ProviderBadge provider={r.provider} />
            <ProjectBadge name={r.project} />
          </span>
        </span>
      </span>
      <span style={{ display: "flex", flexDirection: "column", gap: 6 }}>
        <span style={{ fontSize: 16, lineHeight: "22px", fontWeight: 600, color: "var(--ink-muted)" }}>Made {ago(r.createdAt ?? r.firstSeenAt, m.now)}. Not checked yet.</span>
        <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-faint)" }}>Muretto checks it on the next provider check. Reload to see the result.</span>
      </span>
      <span style={{ marginTop: "auto", paddingTop: 14, borderTop: "1px dashed var(--line-strong)", ...mono() }}>{by ? `made ${shortStamp(by.at, m.now)} · ${by.short}` : "made by this session"}</span>
    </Link>
  );
}

export function OpenSection({ m, f }: { m: Model; f: Facts }) {
  const unchecked = f.s.live ? f.unchecked : [];
  const n = f.open.length + f.cantTell.length + unchecked.length;
  // No half-empty rows: two or four cards sit in two columns.
  const grid = n === 2 || n === 4 ? "m-grid-2" : n === 1 ? "" : "m-grid-3";
  return (
    <Section title="Open to anyone" id="open-h" note="Muretto never changes them. You run the fix.">
      <div className={grid} style={n === 1 ? { maxWidth: 560 } : undefined}>
        {f.open.map((r) => (
          <FixCard key={r.id} r={r} m={m} />
        ))}
        {f.cantTell.map((r) => (
          <CantTellCard key={r.id} r={r} m={m} />
        ))}
        {unchecked.map((r) => (
          <NotCheckedCard key={r.id} r={r} m={m} />
        ))}
      </div>
    </Section>
  );
}

// ------------------------------------------------------- transcript only

export function HooksCard() {
  return (
    <section aria-labelledby="hooks-h" style={{ border: "1px dashed var(--control-line)", borderRadius: "var(--radius-2xl)", background: "var(--surface-raised)", overflow: "hidden" }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "16px 22px", borderBottom: "1px dashed var(--line-strong)" }}>
        <h2 id="hooks-h" style={{ margin: 0, fontSize: 17, lineHeight: "22px", fontWeight: 600 }}>
          Hooks were off for this session
        </h2>
        <span className="m-note-mono">so this record may be incomplete</span>
      </div>
      <div className={css.split}>
        <div style={{ display: "flex", gap: 14, padding: "20px 22px" }}>
          <span className="m-tile dashed" style={{ width: 34, height: 34, color: "var(--ink)" }}>
            <Icon name="file" size={16} />
          </span>
          <span style={{ display: "flex", flexDirection: "column", gap: 6, minWidth: 0 }}>
            <span style={{ fontSize: 15, fontWeight: 600 }}>Read from the transcript afterwards</span>
            <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-muted)" }}>
              Muretto didn&apos;t see the calls as they happened. It read the saved transcript on the next sync. Transcripts can miss things, so this session may have made more than what&apos;s listed.
            </span>
          </span>
        </div>
        <div style={{ display: "flex", flexDirection: "column", justifyContent: "center", gap: 12, padding: "20px 22px" }}>
          <span style={{ fontSize: 15, fontWeight: 600 }}>Turn hooks on</span>
          <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-muted)" }}>Run this once on the machine this session ran on. Sessions there are then recorded as they happen.</span>
          <CommandChip command="muretto init" small />
        </div>
      </div>
    </section>
  );
}

// ------------------------------------------------------- made nothing

export function NothingMade({ m, f }: { m: Model; f: Facts }) {
  const s = f.s;
  const facts: [string, string][] = [
    ["Assistant", [assistantName(s.agent), s.agentVersion].filter(Boolean).join(" ")],
    ...(s.repo ? [["Repo", s.repo] as [string, string]] : []),
    ...(s.branch ? [["Branch", s.branch] as [string, string]] : []),
    ["Ran", `${stamp(s.startedAt, m.now)} · ${duration(s.durationMin)}`],
    ["Recorded", s.transcriptOnly ? "from the transcript, afterwards" : "by the hooks, as it happened"],
  ];
  return (
    <section aria-labelledby="left-h" className="m-card" style={{ overflow: "hidden" }}>
      <div className={`${css.split} ${css.solid}`}>
        <div style={{ display: "flex", flexDirection: "column", justifyContent: "center", gap: 14, padding: 28 }}>
          <span className="m-tile ok" style={{ width: 44, height: 44, borderRadius: 12 }}>
            <Icon name="check-circle" size={20} />
          </span>
          <h2 id="left-h" style={{ margin: 0, fontSize: 22, lineHeight: "28px", fontWeight: 600, letterSpacing: "-0.02em" }}>
            Nothing left behind
          </h2>
          <span style={{ fontSize: 14.5, lineHeight: "22px", color: "var(--ink-muted)" }}>
            {s.live
              ? "Muretto hasn't seen a call create a table, bucket, function, branch, preview or env var yet."
              : "Muretto saw no call create a table, bucket, function, branch, preview or env var. There is nothing here to review or close."}
          </span>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 10, padding: 28 }}>
          <span style={{ font: "500 11.5px var(--font-mono)", letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--ink-faint)" }}>What Muretto recorded</span>
          <dl style={{ margin: 0, display: "flex", flexDirection: "column" }}>
            {facts.map(([k, v], i) => (
              <div key={k} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 16, minHeight: 44, borderBottom: i < facts.length - 1 ? "1px solid var(--line)" : undefined }}>
                <dt style={{ fontSize: 14, color: "var(--ink-muted)" }}>{k}</dt>
                <dd style={{ margin: 0, ...mono(13, "var(--ink)"), textAlign: "right" }}>{v}</dd>
              </div>
            ))}
          </dl>
        </div>
      </div>
    </section>
  );
}

// ------------------------------------------------------- what it created

function StateCell({ r, m }: { r: Res; m: Model }) {
  if (r.status === "open") return <Badge tone="exposed">Open to anyone</Badge>;
  if (r.status === "waiting") return <KeepToggle resourceId={r.id} state="pending" />;
  if (r.status === "cant_tell" || r.status === "unconfirmed")
    return <span style={{ display: "inline-flex", alignItems: "center", height: 24, padding: "0 9px", border: "1px dashed var(--control-line)", borderRadius: 6, font: "500 12px var(--font-mono)", color: "var(--ink-muted)" }}>Can&apos;t tell</span>;
  const quiet = (t: string) => <span style={mono(13)}>{t}</span>;
  if (r.status === "gone") return quiet(r.goneAt ? `gone ${day(r.goneAt)}` : "gone");
  if (notCheckedYet(r, m)) return quiet("not checked yet");
  if (r.review?.state === "keep") return quiet("kept · not open");
  if (r.review?.state === "not_needed") return quiet("not needed · not open");
  if (r.review?.state === "pending") return quiet(`not open · review ${day(r.review.dueAt)}`);
  return quiet("not open");
}

function Row({ r, m }: { r: Res; m: Model }) {
  const gone = r.status === "gone";
  const tone = r.status === "open" ? " red" : r.status === "cant_tell" || r.status === "unconfirmed" ? " dashed" : "";
  const waiting = r.status === "waiting";
  return (
    <div className={css.row}>
      <span className={`m-tile${tone}`} style={{ width: 34, height: 34, borderRadius: 9 }}>
        <KindIcon kind={r.kind} size={16} />
      </span>
      <span style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
        <span style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>
          <Link href={`/resources/${encodeURIComponent(r.id)}`} className={css.stretch} style={{ minWidth: 0, font: "500 14.5px var(--font-mono)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", ...(gone ? { color: "var(--ink-faint)", textDecoration: "line-through" } : null) }}>
            {r.name}
          </Link>
          <OpenLink r={r} />
        </span>
        <span style={{ fontSize: 12.5, color: "var(--ink-faint)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
          {waiting ? `${r.kind} · Waiting on you · made ${age(r.ageDays)} ago` : rowSub(r, m)}
        </span>
      </span>
      <span className={css.above} style={{ justifySelf: "end" }}>
        <StateCell r={r} m={m} />
      </span>
      <span className={css.hideNarrow} style={{ display: "inline-flex", alignItems: "center", gap: 6, ...mono(12.5, "var(--ink-muted)") }} title="The agent's own call created it">
        <span style={{ color: "var(--accent)" }}>✓</span>exact
      </span>
      <span className={css.hideNarrow} style={{ ...mono(), textAlign: "right", whiteSpace: "nowrap" }}>
        {shortStamp(r.createdAt ?? r.firstSeenAt, m.now)}
      </span>
      <span className={css.chev2} aria-hidden>
        ›
      </span>
    </div>
  );
}

export function CreatedSection({ m, f }: { m: Model; f: Facts }) {
  const groups = groupByProject(f.rs);
  const big = f.rs.length > 10;
  const title = f.s.live ? "What it made so far" : f.s.transcriptOnly ? "What it made, per the transcript" : "What it created";
  const note = f.s.transcriptOnly ? "found in the transcript" : big ? `${plural(f.rs.length, "thing")} in ${plural(groups.length, "project")} · each tied to its call` : "every one tied to the call that made it";
  return (
    <Section title={title} id="made-h" note={note}>
      <div className={css.list}>
        {groups.map((g) => {
          const problems = g.rows.filter((r) => r.status === "open" || r.status === "waiting" || r.status === "cant_tell" || r.status === "unconfirmed").length;
          const openN = g.rows.filter((r) => r.status === "open").length;
          return (
            <details key={g.key} className={css.group} open={!big || problems > 0}>
              <summary>
                <ProviderBadge provider={g.provider} />
                <ProjectBadge name={g.project} />
                <span style={{ ...mono(12), whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", minWidth: 0 }}>{g.account}</span>
                <span style={{ marginLeft: "auto", display: "inline-flex", alignItems: "center", gap: 12, ...mono(12.5, "var(--ink-muted)"), whiteSpace: "nowrap" }}>
                  {openN ? <span style={{ color: "var(--exposed)" }}>{openN} open</span> : null}
                  {kindSummary(g.rows)}
                </span>
                <span className={css.chev} aria-hidden>
                  ›
                </span>
              </summary>
              {g.rows.map((r) => (
                <Row key={r.id} r={r} m={m} />
              ))}
            </details>
          );
        })}
        {f.s.transcriptOnly ? (
          <div className={css.row} style={{ background: "var(--surface)", borderTop: "1px solid var(--line)" }}>
            <span className="m-tile dashed" style={{ width: 32, height: 32, borderRadius: 8 }}>
              <Icon name="question" size={16} />
            </span>
            <span style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
              <Link href="/resources?filter=unknown" className={css.stretch} style={{ fontSize: 14.5, color: "var(--ink-muted)" }}>
                Anything else it made
              </Link>
              <span style={{ fontSize: 12.5, color: "var(--ink-faint)" }}>The transcript is all Muretto has. Things nobody recorded show under Origin unknown.</span>
            </span>
            <span style={{ justifySelf: "end", display: "inline-flex", alignItems: "center", height: 24, padding: "0 9px", border: "1px dashed var(--control-line)", borderRadius: 6, font: "500 12px var(--font-mono)", color: "var(--ink-muted)" }}>Can&apos;t tell</span>
            <span className={css.hideNarrow} />
            <span className={css.hideNarrow} style={{ ...mono(12.5, "var(--ink-muted)"), textAlign: "right", whiteSpace: "nowrap" }}>Origin unknown</span>
            <span className={css.chev2} aria-hidden>
              ›
            </span>
          </div>
        ) : null}
      </div>
    </Section>
  );
}

/** Small sessions with something gone: one card per resource, like the board. */
export function CreatedCards({ m, f }: { m: Model; f: Facts }) {
  return (
    <Section title={f.s.live ? "What it made so far" : "What it created"} id="made-h" note={`${plural(f.rs.length, f.rs.every((r) => r.kind === f.rs[0].kind) ? f.rs[0].kind : "thing")} · each tied to the call that made it`}>
      <div className={f.rs.length === 3 ? "m-grid-3" : "m-grid-2"} style={{ alignItems: "stretch" }}>
        {f.rs.map((r) => (
          <ResCard key={r.id} r={r} m={m} />
        ))}
      </div>
    </Section>
  );
}

function ResCard({ r, m }: { r: Res; m: Model }) {
  const gone = r.status === "gone";
  const by = madeBy(r);
  const born = r.createdAt ?? r.firstSeenAt;
  const checkedOk = r.findings.some((x) => x.state === "not_exposed");
  const badge = gone ? (
    <span style={{ flex: "0 0 auto", display: "inline-flex", alignItems: "center", height: 24, padding: "0 9px", border: "1px solid var(--line)", borderRadius: 6, font: "500 12px var(--font-mono)", color: "var(--ink-faint)" }}>Gone</span>
  ) : r.status === "open" ? (
    <Badge tone="exposed" />
  ) : r.status === "waiting" ? (
    <span style={{ flex: "0 0 auto", display: "inline-flex", alignItems: "center", height: 24, padding: "0 9px", border: "1px solid var(--line-strong)", borderRadius: 6, font: "500 12px var(--font-mono)", color: "var(--ink)" }}>Waiting on you</span>
  ) : r.status === "cant_tell" || r.status === "unconfirmed" ? (
    <span className="m-sev dashed">can&apos;t tell</span>
  ) : null;
  const [title, text] = gone
    ? [`Removed on ${r.goneAt ? day(r.goneAt) : "a later day"}. Nothing to do.`, `${providerName(r.provider)} stopped listing it. Muretto keeps the record of which session made it.`]
    : r.status === "open"
      ? [r.open[0].what, r.open[0].why]
      : r.status === "cant_tell" || r.status === "unconfirmed"
        ? ["Muretto couldn't check this.", "The provider check failed, so it might be open to anyone or not."]
        : [
            r.status === "waiting" ? `Still there, ${age(r.ageDays)} later. Still needed?` : "Still there. Not open to anyone.",
            checkedOk ? "Muretto checked it: it isn't open to anyone. It stays until someone removes it." : "Muretto found nothing open on it. It stays until someone removes it.",
          ];
  return (
    <div className="m-card" style={{ display: "flex", flexDirection: "column", gap: 18, padding: 22, borderRadius: "var(--radius-2xl)", ...(gone ? { background: "var(--surface)" } : { borderColor: "var(--line-strong)" }) }}>
      <span style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <span className={`m-tile${r.status === "open" ? " red" : gone ? " plain" : ""}`} style={{ width: 38, height: 38, borderRadius: 10, color: gone ? "var(--ink-faint)" : undefined }}>
          <KindIcon kind={r.kind} size={17} />
        </span>
        <span style={{ display: "flex", flexDirection: "column", gap: 5, flexGrow: 1, minWidth: 0 }}>
          <span style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>
            <Link href={`/resources/${encodeURIComponent(r.id)}`} style={{ minWidth: 0, font: "500 15.5px var(--font-mono)", overflowWrap: "anywhere", ...(gone ? { color: "var(--ink-faint)", textDecoration: "line-through" } : null) }}>
              {r.name}
            </Link>
            <OpenLink r={r} />
          </span>
          <span style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 6 }}>
            <span style={{ fontSize: 12.5, color: "var(--ink-faint)" }}>{r.kind}</span>
            <ProviderBadge provider={r.provider} />
            <ProjectBadge name={r.project} />
          </span>
        </span>
        {badge}
      </span>
      <span style={{ display: "flex", flexDirection: "column", gap: 6 }}>
        <span style={{ fontSize: 17, lineHeight: "23px", fontWeight: 600, color: gone ? "var(--ink-muted)" : "var(--ink)" }}>{title}</span>
        <span style={{ fontSize: 14, lineHeight: "21px", color: gone ? "var(--ink-faint)" : "var(--ink-muted)" }}>{text}</span>
      </span>
      {r.status === "waiting" ? (
        <span style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
          <KeepToggle resourceId={r.id} state="pending" />
          <span style={{ fontSize: 13, color: "var(--ink-faint)" }}>Muretto never deletes anything.</span>
        </span>
      ) : null}
      {gone && r.goneAt ? (
        <span style={{ display: "grid", gridTemplateColumns: "auto minmax(0, 1fr) auto minmax(0, 1fr) auto", alignItems: "center", gap: 10, ...mono() }}>
          <span style={{ display: "inline-flex", alignItems: "center", gap: 7 }}>
            <span style={{ width: 7, height: 7, borderRadius: 999, background: "var(--ink-muted)" }} />
            made {day(born)}
          </span>
          <span style={{ height: 1, background: "var(--line-strong)" }} />
          <span>{age(Math.max(1, Math.round((r.goneAt.getTime() - born.getTime()) / 86_400_000)))}</span>
          <span style={{ height: 1, background: "var(--line-strong)" }} />
          <span style={{ display: "inline-flex", alignItems: "center", gap: 7 }}>
            <span style={{ width: 7, height: 7, boxSizing: "border-box", borderRadius: 999, border: "1.5px solid var(--ink-faint)" }} />
            gone {day(r.goneAt)}
          </span>
        </span>
      ) : null}
      <span style={{ marginTop: "auto", display: "flex", alignItems: "center", gap: 9, paddingTop: 14, borderTop: "1px solid var(--line)", ...mono() }}>
        <span>
          made {shortStamp(by?.at ?? born, m.now)}
          {by ? ` · ${by.short}` : ""} · <span style={{ color: "var(--accent)" }}>✓</span> exact
        </span>
        {r.status === "open" ? (
          <Link href={`/resources/${encodeURIComponent(r.id)}`} style={{ marginLeft: "auto", fontSize: 13.5, fontWeight: 500, color: "var(--ink-muted)", whiteSpace: "nowrap" }}>
            How to close it →
          </Link>
        ) : null}
      </span>
    </div>
  );
}

// ------------------------------------------------------- what the agent did

type Step =
  | { kind: "start"; at: Date; text: string; sub?: string }
  | { kind: "call"; call: ReturnType<typeof creatingCalls>[number] }
  | { kind: "none"; text: string }
  | { kind: "end"; at: Date; text: string }
  | { kind: "live"; text: string };

function StepRow({ step, dashed, transcript, startDay }: { step: Step; dashed: boolean; transcript: boolean; startDay: string }) {
  const dot = (style: CSSProperties) => <span className={css.dot} style={style} />;
  const hollow = { background: "var(--surface)", border: "1.5px solid var(--line-strong)" };
  let time = "";
  let marker: ReactNode = dot(hollow);
  let body: ReactNode = null;
  if (step.kind === "start" || step.kind === "end") {
    time = clock(step.at);
    marker = dot(step.kind === "end" ? { background: "var(--ink-muted)" } : hollow);
    body = (
      <span style={{ display: "flex", alignItems: "baseline", gap: 10, flexWrap: "wrap", paddingTop: 5 }}>
        <span style={{ fontSize: 14.5, color: "var(--ink-muted)" }}>{step.text}</span>
        {step.kind === "start" && step.sub ? <span style={mono(12)}>{step.sub}</span> : null}
      </span>
    );
  } else if (step.kind === "none") {
    marker = dot({ background: "var(--surface)", border: "1.5px dashed var(--control-line)" });
    body = <span style={{ alignSelf: "flex-start", display: "inline-flex", alignItems: "center", height: 34, padding: "0 12px", border: "1px dashed var(--line-strong)", borderRadius: "var(--radius-md)", ...mono(12.5, "var(--ink-muted)") }}>{step.text}</span>;
  } else if (step.kind === "live") {
    time = "now";
    marker = <span className={`${css.ping} ${css.big}`} />;
    body = (
      <span className={css.pulse} style={{ alignSelf: "flex-start", display: "inline-flex", alignItems: "center", height: 38, padding: "0 14px", border: "1px dashed var(--accent-line)", borderRadius: "14px 14px 14px 4px", background: "var(--accent-wash)", fontSize: 14.5, color: "var(--ink)" }}>
        {step.text}
      </span>
    );
  } else {
    const c = step.call;
    const open = c.made.some((r) => r.status === "open");
    const allGone = c.made.every((r) => r.status === "gone");
    const tag = allGone ? `gone ${c.made[0].goneAt ? day(c.made[0].goneAt) : ""}`.trim() : c.made.some((r) => r.status === "waiting") ? "waiting on you" : c.made.some((r) => r.status === "cant_tell" || r.status === "unconfirmed") ? "can't tell" : null;
    time = day(c.at) === startDay ? clock(c.at) : `${day(c.at)}\n${clock(c.at)}`;
    marker = dot(open ? { background: "var(--exposed)" } : allGone ? hollow : { background: "var(--ink-muted)" });
    body = (
      <>
        <span style={{ display: "flex", alignItems: "baseline", gap: 10, flexWrap: "wrap" }}>
          <span style={{ font: "500 14px var(--font-mono)", color: "var(--ink)" }}>{c.tool}</span>
          {c.via ? <span style={mono(12)}>{c.via}</span> : null}
          {transcript ? <span style={{ display: "inline-flex", alignItems: "center", height: 20, padding: "0 7px", borderRadius: 5, border: "1px dashed var(--control-line)", font: "500 11.5px var(--font-mono)", color: "var(--ink-muted)" }}>from transcript</span> : null}
          {tag ? <span style={{ display: "inline-flex", alignItems: "center", height: 20, padding: "0 7px", borderRadius: 5, border: "1px solid var(--line-strong)", font: "500 11.5px var(--font-mono)", color: "var(--ink-muted)" }}>{tag}</span> : null}
        </span>
        <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 10 }}>
          <code className={css.excerpt} title={c.excerpt}>
            {c.excerpt}
          </code>
          {c.made.map((r) => (
            <Link key={r.id} href={`/resources/${encodeURIComponent(r.id)}`} style={{ display: "inline-flex", alignItems: "center", gap: 8, ...mono(12.5, "var(--ink-muted)"), whiteSpace: "nowrap" }}>
              → <span style={r.status === "gone" ? { color: "var(--ink-faint)", textDecoration: "line-through" } : { color: "var(--ink)" }}>{r.name}</span>
            </Link>
          ))}
          {open ? <Badge tone="exposed" /> : null}
        </div>
      </>
    );
  }
  return (
    <li className={`${css.step}${dashed ? ` ${css.dashed}` : ""}`}>
      <span className={css.time} style={step.kind === "live" ? { color: "var(--accent)" } : undefined}>
        {time}
      </span>
      <span className={css.rail}>
        <span className={css.railTop} />
        <span style={{ position: "relative", display: "inline-flex" }}>{marker}</span>
        <span className={css.railBottom} />
      </span>
      <div className={css.body}>{body}</div>
    </li>
  );
}

const FOLD_AFTER = 10;

export function Timeline({ f }: { f: Facts }) {
  const s = f.s;
  const startDay = day(s.startedAt);
  const calls = creatingCalls(f.rs);
  const steps: Step[] = [];
  const startStep: Step = { kind: "start", at: s.startedAt, text: "Session started", sub: [s.repo, s.branch].filter(Boolean).join(" · ") || undefined };
  // Evidence times are the record's; a call stamped before the start shows before it.
  const before = calls.filter((c) => c.at < s.startedAt).map((call): Step => ({ kind: "call", call }));
  const callSteps: Step[] = calls.filter((c) => c.at >= s.startedAt).map((call) => ({ kind: "call", call }));
  steps.push(...before, startStep);
  if (!callSteps.length) steps.push({ kind: "none", text: s.live ? "No call has created anything yet" : "No call created anything" });
  let folded: Step[] = [];
  if (callSteps.length > FOLD_AFTER) {
    steps.push(...callSteps.slice(0, 3));
    folded = callSteps.slice(3, -3);
  } else {
    steps.push(...callSteps);
  }
  const tail: Step[] = callSteps.length > FOLD_AFTER ? callSteps.slice(-3) : [];
  const end: Step = s.live
    ? { kind: "live", text: "Still running. New calls show up after the next sync." }
    : { kind: "end", at: s.endedAt ?? s.lastActivityAt ?? s.startedAt, text: s.endedAt ? `Session ended · ${duration(s.durationMin)}` : "Last activity" };
  const dashed = s.transcriptOnly;
  const note = s.live
    ? "calls that made something · newest at the bottom"
    : calls.length
      ? `${plural(calls.length, "call")} that made something · secrets redacted`
      : "only calls that create something are recorded";
  const row = (st: Step, i: number) => <StepRow key={i} step={st} dashed={dashed} transcript={s.transcriptOnly && st.kind === "call"} startDay={startDay} />;
  const first = folded[0]?.kind === "call" ? folded[0].call.at : undefined;
  const last = folded.at(-1);
  const lastAt = last?.kind === "call" ? last.call.at : undefined;
  return (
    <section aria-labelledby="did-h" className="m-section" style={{ gap: 18 }}>
      <div className="m-section-head">
        <h2 id="did-h" className="m-h2">
          What the agent did
        </h2>
        <span className="m-note-mono">{note}</span>
      </div>
      <ol className={`m-card ${css.steps} ${css.stepsCard}`}>
        {steps.map(row)}
        {folded.length ? (
          <li className={`${css.step}${dashed ? ` ${css.dashed}` : ""}`}>
            <span />
            <span className={css.rail}>
              <span className={css.railTop} />
              <span className={css.dot} style={{ background: "var(--surface)", border: "1.5px dashed var(--control-line)" }} />
              <span className={css.railBottom} />
            </span>
            <div className={css.body}>
              <details className={css.fold}>
                <summary>
                  <span style={{ color: "var(--ink)" }}>
                    <span className={css.closed}>{plural(folded.length, "more call")}</span>
                    <span className={css.opened}>Hide {plural(folded.length, "call")}</span>
                  </span>
                  {first && lastAt ? <span>{`${clock(first)} – ${clock(lastAt)}`}</span> : null}
                  <span className={css.chev} aria-hidden>
                    ›
                  </span>
                </summary>
                <ol className={css.steps} style={{ marginTop: 8, marginLeft: -94 }}>
                  {folded.map((st, i) => (
                    <StepRow key={i} step={st} dashed={dashed} transcript={s.transcriptOnly} startDay={startDay} />
                  ))}
                </ol>
              </details>
            </div>
          </li>
        ) : null}
        {tail.map((st, i) => row(st, i + 1000))}
        {row(end, 9999)}
      </ol>
    </section>
  );
}
