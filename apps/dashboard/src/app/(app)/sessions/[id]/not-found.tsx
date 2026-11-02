import Link from "next/link";
import { Button, Icon } from "@/components/ds";
import { SessionAvatar } from "@/components/brand";
import { loadModel } from "@/lib/data";
import { when } from "@/lib/format";
import type { Model } from "@/lib/model";

export default async function SessionNotFound() {
  let m: Model | undefined;
  try {
    m = await loadModel();
  } catch {
    // Still show the calm page; the list just stays out.
  }
  const latest = m?.sessions.slice(0, 3) ?? [];
  return (
    <div className="m-center">
      <div className="m-card m-alert" style={{ maxWidth: 520, borderRadius: "var(--radius-2xl)" }}>
        <span className="m-tile" style={{ width: 44, height: 44, borderRadius: 12, color: "var(--ink)" }}>
          <Icon name="search" size={20} />
        </span>
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <p className="m-eyebrow">Agent session</p>
          <h1>
            <span>We can&apos;t find this session.</span>
            <span className="q">It may not have synced yet.</span>
          </h1>
          <span style={{ fontSize: 15, lineHeight: "23px", color: "var(--ink-muted)" }}>
            Sessions show up after the machine they ran on syncs. If that machine is asleep or offline, run <span className="mono" style={{ color: "var(--ink)" }}>muretto sync now</span> on it. The link may also have a typo.
          </span>
        </div>
        {latest.length ? (
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <span style={{ font: "500 11.5px var(--font-mono)", letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--ink-faint)" }}>Your latest sessions</span>
            <div style={{ border: "1px solid var(--line)", borderRadius: "var(--radius-lg)", overflow: "hidden", background: "var(--surface)" }}>
              {latest.map((s, i) => (
                <Link key={s.id} href={`/sessions/${s.id}`} className="m-row" style={{ minHeight: 56, padding: "0 14px", gap: 12, borderBottom: i < latest.length - 1 ? "1px solid var(--line)" : 0 }}>
                  <SessionAvatar id={s.id} agent={s.agent} size={28} />
                  <span className="m-row-main" style={{ gap: 2 }}>
                    <span className="m-row-title" style={{ fontSize: 14 }}>
                      {s.title}
                    </span>
                    <span style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)" }}>
                      {s.short} · {s.live ? "running now" : when(s.startedAt, m!.now)}
                    </span>
                  </span>
                  <span aria-hidden style={{ fontSize: 16, color: "var(--ink-faint)" }}>
                    ›
                  </span>
                </Link>
              ))}
            </div>
          </div>
        ) : null}
        <div className="m-half">
          <Button variant="secondary" href="/settings">
            See your machines
          </Button>
          <Button variant="primary" href="/">
            Back to overview
          </Button>
        </div>
        {m ? <span style={{ paddingTop: 16, borderTop: "1px solid var(--line)", font: "12.5px var(--font-mono)", color: "var(--ink-faint)" }}>not in your {m.sessions.length} synced sessions</span> : null}
      </div>
    </div>
  );
}
