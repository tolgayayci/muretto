import type { Metadata } from "next";
import Link from "next/link";
import { Icon } from "@/components/ds";
import { KindIcon, ProviderBadge, ProviderLogo, SessionAvatar } from "@/components/brand";
import { CommandChip } from "@/components/client";
import { CantTellCard, Checked, Count, EmptyState, IssueCard, PageHeader, ProblemBox, Section, SessionRow } from "@/components/ui";
import { loadModel, loadViewer } from "@/lib/data";
import { age, plural, severitySummary, word } from "@/lib/format";
import { problems as setupProblems, type Model } from "@/lib/model";
import s from "./_components/overview.module.css";

export const metadata: Metadata = { title: "Overview" };

export default async function OverviewPage({ searchParams }: { searchParams: Promise<{ project?: string }> }) {
  const [m, viewer, sp] = await Promise.all([loadModel(), loadViewer(), searchParams]);
  if (m.state === "new") return <NewHere name={viewer.user.name} />;
  if (m.state === "waiting") return <Connected m={m} />;
  return <Normal m={m} project={sp.project} />;
}

function NewHere({ name }: { name: string }) {
  const first = name.split(/\s+/)[0] || "there";
  const steps = [
    { title: "Install the CLI", note: "Or with Homebrew: brew install tolgayayci/tap/muretto", cmd: "npm i -g muretto" },
    { title: "Add the hooks and connect providers", note: "Backs up your Claude Code settings first. Read-only tokens.", cmd: "muretto init" },
    { title: "Sign the CLI in", note: "You approve a short code in this browser.", cmd: "muretto login" },
    { title: "Turn on sync", note: "Shows every field before anything is sent.", cmd: "muretto sync on" },
  ];
  return (
    <div className="m-page" style={{ gap: 32 }}>
      <PageHeader eyebrow={`Welcome, ${first}`} title="Connect your first machine." quiet="Then every agent session shows up here." />
      <div className="m-split">
        <section aria-labelledby="steps-h" className="m-card" style={{ padding: 26, display: "flex", flexDirection: "column", gap: 6 }}>
          <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 12, paddingBottom: 10 }}>
            <h2 id="steps-h" style={{ margin: 0, fontSize: 18, lineHeight: "24px", fontWeight: 600 }}>
              Four commands, about a minute
            </h2>
            <span className="m-note-mono">macOS · Linux</span>
          </div>
          <ol style={{ margin: 0, padding: 0, listStyle: "none", display: "flex", flexDirection: "column" }}>
            {steps.map((s, i) => (
              <li key={s.cmd} style={{ display: "grid", gridTemplateColumns: "28px minmax(0, 1fr)", gap: 14, padding: "14px 0", borderTop: "1px solid var(--line)" }}>
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
            <span className="m-dot off" style={{ boxShadow: "0 0 0 4px var(--surface-hover)" }} />
            Waiting for your first machine. Reload after the first sync.
          </div>
        </section>
        <section aria-labelledby="see-h" className="m-card" style={{ background: "var(--surface-sunken)", padding: 26, display: "flex", flexDirection: "column", gap: 18 }}>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12 }}>
            <h2 id="see-h" style={{ margin: 0, fontSize: 18, lineHeight: "24px", fontWeight: 600 }}>
              What you&apos;ll see
            </h2>
            <span className="m-sev" style={{ color: "var(--ink-faint)" }}>
              example
            </span>
          </div>
          <div aria-hidden style={{ display: "flex", flexDirection: "column", gap: 14, opacity: 0.6 }}>
            <span className="m-card" style={{ display: "flex", flexDirection: "column", gap: 12, padding: 16 }}>
              <span className="m-wrap-narrow" style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <span className="m-tile red" style={{ width: 30, height: 30, borderRadius: 8 }}>
                  <KindIcon kind="table" size={15} />
                </span>
                <span style={{ font: "500 14px var(--font-mono)" }}>customers</span>
                <span style={{ marginLeft: "auto", font: "500 11.5px var(--font-mono)", color: "var(--ink-muted)" }}>critical</span>
              </span>
              <span style={{ fontSize: 14.5, fontWeight: 600 }}>Anyone can read every row.</span>
            </span>
            <span className="m-card" style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 16px" }}>
              <SessionAvatar id="8f2a-example" agent="claude-code" size={30} />
              <span style={{ display: "flex", flexDirection: "column", gap: 2, minWidth: 0 }}>
                <span style={{ fontSize: 14 }}>add customer import with file uploads</span>
                <span style={{ font: "11.5px var(--font-mono)", color: "var(--ink-faint)" }}>Claude Code · acme/web</span>
              </span>
            </span>
          </div>
          <ul style={{ margin: "auto 0 0", padding: "16px 0 0", listStyle: "none", borderTop: "1px solid var(--line)", display: "flex", flexDirection: "column", gap: 12 }}>
            {(
              [
                ["lock", "Muretto only reads. It never changes your accounts."],
                ["terminal", "The CLI works on its own, without this page."],
                ["search", "Nothing is sent until you turn sync on."],
              ] as const
            ).map(([icon, text]) => (
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

function Connected({ m }: { m: Model }) {
  const providers = m.checks.map((g) => ({ title: g.provider === "supabase" ? "Supabase" : g.provider === "vercel" ? "Vercel" : g.provider, sub: g.account, ok: g.ok }));
  const kinds = new Map<string, Map<string, number>>();
  for (const r of m.unknownOrigin) {
    const k = kinds.get(r.provider) ?? new Map<string, number>();
    k.set(r.kind, (k.get(r.kind) ?? 0) + 1);
    kinds.set(r.provider, k);
  }
  return (
    <div className="m-page" style={{ gap: 32 }}>
      <PageHeader eyebrow="Overview · Personal" title="You're connected." quiet="Start an agent session to see it here." right={<Checked m={m} />} />
      <div className="m-card" style={{ display: "grid", gridTemplateColumns: `repeat(${providers.length + 1}, minmax(0, 1fr))`, overflow: "hidden" }}>
        {[{ title: "This machine syncs", sub: "hooks on", ok: true }, ...providers].map((s, i) => (
          <span key={s.title + i} style={{ display: "flex", alignItems: "center", gap: 12, minWidth: 0, padding: "18px 20px", borderRight: i < providers.length ? "1px solid var(--line)" : undefined }}>
            <span className={`m-tile ${s.ok ? "ok" : ""}`} style={{ width: 30, height: 30, borderRadius: 999 }}>
              {s.ok ? "✓" : "!"}
            </span>
            <span style={{ display: "flex", flexDirection: "column", gap: 2, minWidth: 0 }}>
              <span style={{ fontSize: 14.5, fontWeight: 500 }}>{s.title}</span>
              <span style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)" }}>{s.sub}</span>
            </span>
          </span>
        ))}
      </div>
      <div className="m-grid-2" style={{ alignItems: "stretch" }}>
        <section aria-labelledby="ses-h" className="m-section">
          <h2 id="ses-h" className="m-h2">
            Your recent sessions
          </h2>
          <EmptyState icon="terminal" title="No agent sessions yet" text="Open Claude Code in any repo and ask it to build something. The session shows up here after the next sync." minHeight={340}>
            <CommandChip command="claude" />
          </EmptyState>
        </section>
        <section aria-labelledby="found-h" className="m-section">
          <h2 id="found-h" className="m-h2">
            Already in your accounts
          </h2>
          <div className="m-card" style={{ flexGrow: 1, padding: 24, display: "flex", flexDirection: "column", gap: 20 }}>
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              <span style={{ display: "flex", alignItems: "baseline", gap: 10 }}>
                <span className="m-stat-value">{m.unknownOrigin.length}</span>
                <span style={{ fontSize: 15, color: "var(--ink-muted)" }}>resources were here before Muretto</span>
              </span>
              <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-faint)" }}>No recorded session made them, so their origin is unknown. Muretto still checks them.</span>
            </div>
            {m.open.length === 0 ? (
              <div className="m-banner ok" style={{ padding: "12px 14px", borderRadius: "var(--radius-lg)", fontSize: 14 }}>
                <span style={{ color: "var(--accent)" }}>✓</span> None of them are open to anyone.
              </div>
            ) : null}
            <div style={{ display: "flex", flexDirection: "column", borderTop: "1px solid var(--line)" }}>
              {[...kinds.entries()].map(([provider, k]) => (
                <div key={provider} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "11px 0", borderBottom: "1px solid var(--line)" }}>
                  <ProviderBadge provider={provider} />
                  <span className="m-note-mono" style={{ textAlign: "right" }}>
                    {[...k.entries()].map(([kind, n]) => plural(n, kind)).join(" · ")}
                  </span>
                </div>
              ))}
            </div>
            <Link href="/resources" className="mu-btn mu-btn-secondary mu-btn-sm" style={{ marginTop: "auto", justifyContent: "center" }}>
              See them in My resources
            </Link>
          </div>
        </section>
      </div>
    </div>
  );
}

function Normal({ m, project }: { m: Model; project?: string }) {
  const problems = setupProblems(m);
  const many = m.projects.filter((p) => p.provider && p.resources.some((r) => r.kind === "project")).length > 3;
  const pick = many && project ? m.projects.find((p) => p.name === project) : undefined;
  const open = pick ? m.open.filter((r) => r.projectId === pick.id) : m.open;
  const cantTell = pick ? m.cantTell.filter((r) => r.projectId === pick.id) : m.cantTell;
  const openProjects = new Set(m.open.map((r) => r.projectId)).size;
  const projectsTotal = m.projects.filter((p) => p.resources.some((r) => r.kind === "project")).length;
  const sessionsToday = m.sessions.filter((s) => s.openCount > 0);
  const madeToday = new Set(m.open.map((r) => r.sessionId)).size === 1 && m.open[0]?.ageDays === 0;

  const title =
    m.state === "clear"
      ? "Nothing is open to anyone."
      : `${plural(m.open.length, "thing")} ${m.open.length === 1 ? "is" : "are"} open to anyone.`;
  const quiet =
    m.state === "clear"
      ? `Your agents made ${plural(m.agentMade.filter((r) => r.ageDays < 7).length, "thing")} this week. All private.`
      : cantTell.length && problems.length
        ? `${cantTell.length} more might be. Some checks failed.`
        : many
          ? `In ${openProjects} of your ${projectsTotal} projects.`
          : madeToday
            ? m.open.length === 1
              ? "Your agent made it today."
              : `Your agent made ${m.open.length === 2 ? "both" : `all ${word(m.open.length)}`} today.`
            : sessionsToday.length === 1
              ? "They came from one agent session."
              : `From ${plural(sessionsToday.length, "agent session")}.`;

  return (
    <div className="m-page">
      <PageHeader eyebrow="Overview · Personal" title={title} quiet={quiet} right={<Checked m={m} prefix={problems.length ? "partly checked" : "checked"} />} />

      <ProblemBox problems={problems} />

      <nav aria-label="Summary" className="m-card m-strip">
        {m.state === "clear" ? (
          <Count label="Open to anyone" value={0} help="everything is private" icon="check" tone="ok" href="/resources" />
        ) : (
          <Count
            label="Open to anyone"
            value={m.open.length}
            help={cantTell.length ? `+${cantTell.length} Muretto couldn't check` : severitySummary(m.open.map((r) => r.open[0].severity))}
            icon="globe"
            tone="red"
            href="#open-h"
          />
        )}
        <Count label="Waiting on you" value={m.waiting.length} help={m.waiting.length ? `oldest ${age(m.waiting[0].review?.overdueDays ?? 0)} overdue` : "nothing to decide"} icon="clock" href="/resources?filter=waiting" />
        <Count label="Made by your agents" value={m.agentMade.length} help={many ? `still running in ${plural(projectsTotal, "project")}` : "still running"} icon="sparkle" href="/resources?filter=agent" />
        <Count label="Origin unknown" value={m.unknownOrigin.length} help="no session made these" icon="question" href="/resources?filter=unknown" />
      </nav>

      {many ? <Projects m={m} pick={pick?.name} /> : null}

      {m.state === "clear" ? (
        <section aria-label="Open to anyone" className="m-banner ok">
          <span style={{ flex: "0 0 40px", height: 40, borderRadius: 999, background: "var(--accent)", color: "var(--on-accent)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 18, fontWeight: 600 }}>✓</span>
          <span className="m-banner-text">
            <span className="m-banner-title">Nothing is open to anyone.</span>
            <span className="m-banner-sub">Muretto checked tables, buckets, previews and browser env vars in {plural(projectsTotal, "project")}.</span>
          </span>
          <Link href="/providers" className="m-link-mono">
            What gets checked →
          </Link>
        </section>
      ) : (
        <Section title={pick ? `Open to anyone in ${pick.name}` : "Open to anyone"} id="open-h" note="anyone on the internet can reach these">
          {open.length + cantTell.length === 0 ? (
            <div className="m-banner ok" style={{ padding: "18px 20px" }}>
              <span style={{ color: "var(--accent)", fontWeight: 600 }}>✓</span> Nothing in {pick?.name} is open to anyone.
            </div>
          ) : (
            <div className="m-grid-3">
              {open.map((r) => (
                <IssueCard key={r.id} r={r} m={m} compact={many} />
              ))}
              {cantTell.map((r) => (
                <CantTellCard key={r.id} r={r} m={m} />
              ))}
            </div>
          )}
        </Section>
      )}

      <Section title="Your recent sessions" id="ses-h" note={m.sessions.length > 5 ? `latest 5 of ${m.sessions.length}` : undefined}>
        <div className="m-list" style={{ background: "var(--surface-raised)", borderColor: "var(--line)" }}>
          {m.sessions.slice(0, 5).map((x) => (
            <SessionRow key={x.id} s={x} m={m} />
          ))}
        </div>
      </Section>
    </div>
  );
}

function Projects({ m, pick }: { m: Model; pick?: string }) {
  const projects = m.projects.filter((p) => p.resources.some((r) => r.kind === "project"));
  return (
    <Section title="Your projects" id="proj-h" link={pick ? { href: "/", label: "Show all projects ×" } : undefined} note={pick ? undefined : "pick one to filter"}>
      <div className={s.projects}>
        {projects.map((p) => {
          const on = p.name === pick;
          return (
            <Link
              key={p.id}
              href={on ? "/" : `/?project=${encodeURIComponent(p.name)}`}
              aria-current={on ? "true" : undefined}
              className="m-card"
              style={{ display: "flex", flexDirection: "column", gap: 14, padding: "16px 18px", color: "var(--ink)", opacity: pick && !on ? 0.55 : 1, borderColor: on ? "var(--accent-line)" : undefined }}
            >
              <span style={{ display: "flex", alignItems: "center", gap: 12 }}>
                <span className="m-tile" style={{ width: 34, height: 34 }}>
                  <ProviderLogo provider={p.provider} size={17} />
                </span>
                <span style={{ display: "flex", flexDirection: "column", gap: 2, flexGrow: 1, minWidth: 0 }}>
                  <span style={{ font: "500 14.5px var(--font-mono)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }} title={p.name}>
                    {p.name}
                  </span>
                  <span style={{ fontSize: 12.5, color: "var(--ink-faint)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{p.account}</span>
                </span>
                {p.open > 0 ? (
                  <span style={{ flex: "none", padding: "3px 9px", borderRadius: 999, background: "var(--exposed-wash)", border: "1px solid var(--exposed-line)", font: "500 12px var(--font-mono)", color: "var(--exposed-ink)", whiteSpace: "nowrap" }}>{p.open} open</span>
                ) : (
                  <span style={{ flex: "none", font: "500 12px var(--font-mono)", color: "var(--accent)", whiteSpace: "nowrap" }}>✓ private</span>
                )}
              </span>
              <span style={{ display: "flex", gap: 16, paddingTop: 12, borderTop: "1px solid var(--line)", font: "12.5px var(--font-mono)", color: "var(--ink-faint)" }}>
                <span>
                  <span style={{ color: "var(--ink)" }}>{p.resources.filter((r) => r.origin === "agent" && r.existence !== "gone").length}</span> running
                </span>
                <span>
                  <span style={{ color: "var(--ink)" }}>{p.waiting}</span> waiting
                </span>
              </span>
            </Link>
          );
        })}
      </div>
    </Section>
  );
}
