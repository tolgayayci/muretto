import type { Metadata } from "next";
import Link from "next/link";
import { CheckList, Icon, Mark } from "@/components/ds";
import { GitHubMark } from "@/components/github-button";
import { CommandChip } from "@/components/client";
import { PageHeader } from "@/components/ui";
import { UserAvatar } from "@/components/user-avatar";
import { initials } from "@/lib/auth";
import { loadMachines, loadModel, loadViewer } from "@/lib/data";
import { plural } from "@/lib/format";
import { SettingsDialog, type Confirm } from "./_components/dialog";
import { machineViews, type MachineView } from "./_components/machines";

export const metadata: Metadata = { title: "Settings" };

// The short version. The full field list is internal/syncclient/SYNC_PAYLOAD.md.
const SENT = ["Session titles, redacted", "Repo name and branch", "What was created, and what's open"];
const NEVER = ["Tokens, keys and env values", "Prompts, transcripts and code", "Local paths (only a hash is sent)"];

const card = { padding: 24, display: "flex", flexDirection: "column", gap: 18, borderRadius: "var(--radius-2xl)", minWidth: 0 } as const;
const h2 = { margin: 0, fontSize: 17, lineHeight: "22px", fontWeight: 600 } as const;
const btnLink = "mu-btn mu-btn-secondary mu-btn-sm";
/** List rows that wrap their end part under the text on narrow screens. */
const row = { flexWrap: "wrap", rowGap: 12, padding: "16px 20px" } as const;
const rowMain = { flex: "1 1 240px" } as const;

export default async function SettingsPage({ searchParams }: { searchParams: Promise<{ confirm?: string | string[] }> }) {
  const [viewer, list, m, sp] = await Promise.all([loadViewer(), loadMachines(), loadModel(), searchParams]);
  const machines = machineViews(list, m.now);
  const stale = machines.filter((x) => x.state === "stale");
  const user = viewer.user;
  const mine = initials(user.name);

  const ask = typeof sp.confirm === "string" ? sp.confirm : undefined;
  const target = ask ? machines.find((x) => x.id === ask) : undefined;
  const confirm: Confirm | null = target
    ? {
        kind: "machine",
        id: target.id,
        name: target.name,
        facts: [
          ["machine", target.name],
          ["signed in", target.signedIn],
          ["last sync", target.lastSync ?? "never"],
        ],
      }
    : ask === "signout"
      ? { kind: "signout", name: user.name, login: user.login, initials: mine, avatarUrl: user.avatarUrl }
      : null;

  const upToDate = machines.filter((x) => x.state === "ok").length;
  const head = headline(machines, stale);

  return (
    <div className="m-page" style={{ gap: 32 }}>
      <PageHeader
        eyebrow="Settings · Personal"
        title={head.title}
        quiet={head.quiet}
        right={
          <span className="m-checked">
            <span className={`m-dot${machines.length && !stale.length && machines[0].lastSync ? "" : " off"}`} />
            {!machines.length ? "no machine yet" : stale.length ? `${upToDate} of ${plural(machines.length, "machine")} up to date` : machines[0].lastSync ? `synced ${machines[0].lastSync}` : "no sync yet"}
          </span>
        }
      />

      {stale.length ? <StaleBox stale={stale} /> : null}

      <section aria-labelledby="acct-h" className="m-section">
        <div className="m-section-head">
          <h2 id="acct-h" className="m-h2">
            Account
          </h2>
        </div>
        <div className="m-list">
          <div className="m-row wrap" style={row}>
            <UserAvatar initials={mine} url={user.avatarUrl} size={44} />
            <span className="m-row-main" style={rowMain}>
              <span style={{ fontSize: 16, fontWeight: 600, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{user.name}</span>
              <span className="m-row-sub mono" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
                <GitHubMark size={12} />@{user.login} · signed in with GitHub
              </span>
            </span>
            <Link href="/settings?confirm=signout" scroll={false} className={btnLink}>
              Sign out
            </Link>
          </div>
          <div className="m-row wrap" style={row}>
            <span className="m-tile" style={{ width: 44, height: 44, borderRadius: 12, color: "var(--accent)" }}>
              <Mark size={22} />
            </span>
            <span className="m-row-main" style={rowMain}>
              <span style={{ fontSize: 16, fontWeight: 600 }}>Solo workspace</span>
              <span className="m-row-sub">
                Only you see these sessions{machines.length ? ` · ${plural(m.sessions.length, "session")} · ${plural(machines.length, "machine")}` : ""}
              </span>
            </span>
            <span style={{ display: "inline-flex", alignItems: "center", gap: 8, fontSize: 13.5, color: "var(--ink-faint)" }}>
              <Icon name="users" size={15} />
              Teams <span className="m-soon">soon</span>
            </span>
          </div>
        </div>
      </section>

      <section aria-labelledby="mach-h" className="m-section">
        <div className="m-section-head">
          <h2 id="mach-h" className="m-h2">
            Your machines
          </h2>
          {machines.length ? <span className="m-note-mono">{plural(machines.length, "machine")}</span> : null}
        </div>
        {machines.length ? (
          <div className="m-list">
            {machines.map((x) => (
              <MachineRow key={x.id} x={x} />
            ))}
            <AddMachineRow />
          </div>
        ) : (
          <FirstMachine />
        )}
      </section>

      <section aria-labelledby="data-h" className="m-section">
        <div className="m-section-head" style={{ flexWrap: "wrap" }}>
          <h2 id="data-h" className="m-h2">
            What leaves your machines
          </h2>
          <span className="m-note-mono">only with sync on</span>
        </div>
        <div className="m-list">
          <div className="m-grid-2" style={{ gap: 20, padding: "20px 20px 22px" }}>
            <div style={{ display: "flex", flexDirection: "column", gap: 14, minWidth: 0 }}>
              <h3 style={{ ...h2, display: "flex", alignItems: "center", gap: 10, fontSize: 15 }}>
                <span className="m-tile ok" style={{ width: 28, height: 28 }}>
                  <Icon name="check" size={14} />
                </span>
                Sent
              </h3>
              <CheckList items={SENT} />
            </div>
            <div style={{ display: "flex", flexDirection: "column", gap: 14, minWidth: 0 }}>
              <h3 style={{ ...h2, display: "flex", alignItems: "center", gap: 10, fontSize: 15 }}>
                <span className="m-tile" style={{ width: 28, height: 28 }}>
                  <Icon name="lock" size={14} />
                </span>
                Never sent
              </h3>
              <ul className="mu-checks">
                {NEVER.map((n) => (
                  <li key={n} style={{ color: "var(--ink-muted)" }}>
                    <span className="mu-ck" aria-hidden style={{ background: "var(--surface-hover)", color: "var(--ink-faint)" }}>
                      <Icon name="x" size={11} strokeWidth={2.6} />
                    </span>
                    <span>{n}</span>
                  </li>
                ))}
              </ul>
            </div>
          </div>
          <div className="m-row" style={{ ...row, minHeight: 0, padding: "12px 20px", borderTop: "1px solid var(--line)", background: "var(--surface-raised)" }}>
            <span style={{ flex: "1 1 200px", fontSize: 14, color: "var(--ink-faint)" }}>Turn it off on a machine</span>
            <CommandChip command="muretto sync off" small prompt={false} />
          </div>
        </div>
      </section>

      <SettingsDialog confirm={confirm} />
    </div>
  );
}

function headline(machines: MachineView[], stale: MachineView[]): { title: string; quiet: string } {
  if (!machines.length) return { title: "Your account is ready.", quiet: "Add a machine to start syncing." };
  if (stale.length === 1) {
    const s = stale[0];
    return { title: `${s.name} hasn't synced in ${plural(s.quietDays, "day")}.`, quiet: "Its new sessions won't show here until it does." };
  }
  if (stale.length > 1) return { title: `${plural(stale.length, "machine")} haven't synced in days.`, quiet: "Their new sessions won't show here until they do." };
  return { title: "Your account and machines.", quiet: "And exactly what leaves them." };
}

function StaleBox({ stale }: { stale: MachineView[] }) {
  return (
    <section aria-labelledby="setup-h" className="m-list" style={{ background: "var(--surface-raised)" }}>
      <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "16px 20px", borderBottom: "1px solid var(--line)" }}>
        <h2 id="setup-h" style={h2}>
          {stale.length === 1 ? "1 machine hasn't synced lately" : `${stale.length} machines haven't synced lately`}
        </h2>
        <span className="m-note-mono">until then, {stale.length === 1 ? "its" : "their"} sessions here may be out of date</span>
      </div>
      {stale.map((s) => (
        <div key={s.id} style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 14, padding: "16px 20px", borderBottom: "1px solid var(--line)" }}>
          <span className="m-tile" style={{ width: 34, height: 34, color: "var(--ink)" }}>
            <Icon name="laptop" size={16} />
          </span>
          <span style={{ display: "flex", flexDirection: "column", gap: 3, flex: "1 1 320px", minWidth: 0 }}>
            <span style={{ fontSize: 15, fontWeight: 600 }}>
              No sync from {s.name} since {s.lastSyncDay}
            </span>
            <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-muted)" }}>If it&apos;s asleep or offline, there&apos;s nothing to do. If sync is off there, run this on it.</span>
          </span>
          <CommandChip command="muretto sync on" small />
        </div>
      ))}
    </section>
  );
}

function MachineRow({ x }: { x: MachineView }) {
  const ok = x.state === "ok";
  return (
    <article aria-label={x.name} className="m-row wrap" style={row}>
      <span className="m-tile" style={{ width: 40, height: 40, borderRadius: 10, color: "var(--ink)" }}>
        <Icon name="laptop" size={17} />
      </span>
      <span className="m-row-main" style={rowMain}>
        <span style={{ fontSize: 15, fontWeight: 600, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{x.name}</span>
        <span className="m-row-sub mono">Muretto CLI · signed in {x.signedIn}</span>
      </span>
      <span
        style={{
          flex: "0 0 auto",
          display: "inline-flex",
          alignItems: "center",
          gap: 7,
          height: 28,
          padding: "0 11px",
          borderRadius: 999,
          font: "500 12px var(--font-mono)",
          whiteSpace: "nowrap",
          ...(ok ? { border: "1px solid var(--accent-line)", background: "var(--accent-wash)", color: "var(--accent)" } : { border: "1px dashed var(--control-line)", color: "var(--ink)" }),
        }}
      >
        <span className={`m-dot${ok ? "" : " off"}`} />
        {ok ? `synced ${x.lastSync}` : x.state === "stale" ? `needs a look · last sync ${x.lastSync}` : "not synced yet"}
      </span>
      <Link href={`/settings?confirm=${encodeURIComponent(x.id)}`} scroll={false} className={btnLink} aria-label={`Sign ${x.name} out`}>
        Sign out
      </Link>
    </article>
  );
}

function AddMachineRow() {
  return (
    <article aria-label="Add a machine" className="m-row wrap" style={{ ...row, background: "var(--surface-raised)" }}>
      <span className="m-tile dashed" style={{ width: 40, height: 40, borderRadius: 10 }}>
        <Icon name="laptop" size={17} />
      </span>
      <span className="m-row-main" style={rowMain}>
        <span style={{ fontSize: 15, fontWeight: 600 }}>Add a machine</span>
        <span className="m-row-sub">Run these on it. The CLI must be installed there.</span>
      </span>
      <span style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
        <CommandChip command="muretto login" small />
        <CommandChip command="muretto sync on" small />
      </span>
    </article>
  );
}

function FirstMachine() {
  const steps = [
    { title: "Sign the CLI in", note: "You approve a short code in this browser.", cmd: "muretto login" },
    { title: "Turn on sync", note: "It lists every field before anything is sent.", cmd: "muretto sync on" },
  ];
  return (
    <div className="m-card" style={{ ...card, padding: 26, gap: 20 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <span className="m-tile dashed" style={{ width: 36, height: 36, borderRadius: 10 }}>
          <Icon name="laptop" size={16} />
        </span>
        <span style={{ display: "flex", flexDirection: "column", gap: 2, flexGrow: 1, minWidth: 0 }}>
          <span style={{ fontSize: 15.5, fontWeight: 600 }}>Add a machine</span>
          <span style={{ fontSize: 13.5, color: "var(--ink-faint)" }}>Run these on each machine you code on. The CLI must be installed there.</span>
        </span>
        <Link href="/" className="m-link-mono" style={{ flex: "0 0 auto" }}>
          Install steps →
        </Link>
      </div>
      <ol className="m-grid-2" style={{ margin: 0, padding: 0, listStyle: "none", gap: 14 }}>
        {steps.map((s, i) => (
          <li key={s.cmd} style={{ display: "flex", flexDirection: "column", gap: 14, padding: 18, border: "1px solid var(--line)", borderRadius: "var(--radius-xl)", background: "var(--surface)", minWidth: 0 }}>
            <span style={{ display: "flex", alignItems: "center", gap: 12 }}>
              <span style={{ flex: "0 0 26px", height: 26, boxSizing: "border-box", borderRadius: 999, border: "1px solid var(--line-strong)", display: "inline-flex", alignItems: "center", justifyContent: "center", font: "600 12px var(--font-mono)", color: "var(--ink-muted)" }}>{i + 1}</span>
              <span style={{ display: "flex", flexDirection: "column", gap: 2, minWidth: 0 }}>
                <span style={{ fontSize: 15, fontWeight: 600 }}>{s.title}</span>
                <span style={{ fontSize: 13.5, color: "var(--ink-faint)" }}>{s.note}</span>
              </span>
            </span>
            <CommandChip command={s.cmd} />
          </li>
        ))}
      </ol>
      <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "14px 16px", border: "1px dashed var(--line-strong)", borderRadius: "var(--radius-lg)", fontSize: 14, color: "var(--ink-muted)" }}>
        <span className="m-dot off" style={{ width: 8, height: 8, boxShadow: "0 0 0 4px var(--surface-hover)" }} />
        Waiting for your first machine. Reload after it signs in.
      </div>
    </div>
  );
}
