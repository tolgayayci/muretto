"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Icon, Mark, type IconName } from "@/components/ds";
import { SessionAvatar } from "@/components/brand";
import { WorkspaceMenu } from "@/components/workspace-menu";

export interface SidebarProps {
  name: string;
  login: string;
  initials: string;
  /** GitHub avatar; initials are shown without one. */
  avatarUrl?: string;
  counts: { open: number; resources: number };
  /** Already formatted on the server, so client and server render the same text. */
  synced: string | null;
  connected: boolean;
  sessions: { id: string; agent: string; title: string; meta: string; open: boolean }[];
}

const NAV: { href: string; label: string; icon: IconName; count?: keyof SidebarProps["counts"]; red?: boolean }[] = [
  { href: "/", label: "Overview", icon: "overview", count: "open", red: true },
  { href: "/resources", label: "My resources", icon: "database", count: "resources" },
  { href: "/providers", label: "Providers", icon: "key" },
  { href: "/settings", label: "Settings", icon: "sliders" },
];

function isActive(path: string, href: string) {
  return href === "/" ? path === "/" : path === href || path.startsWith(`${href}/`);
}

export function Sidebar(p: SidebarProps) {
  const path = usePathname();
  return (
    <aside className="m-side" aria-label="Sidebar">
      <div className="m-side-inner">
        <Link href="/" aria-label="muretto home" style={{ display: "flex", alignItems: "center", padding: "2px 8px 6px" }}>
          <Mark size={24} wordmark />
        </Link>

        <WorkspaceMenu name={p.name} login={p.login} initials={p.initials} avatarUrl={p.avatarUrl} />

        <nav aria-label="Main" style={{ display: "flex", flexDirection: "column", gap: 2 }}>
          {NAV.map((n) => {
            const on = isActive(path, n.href);
            const c = n.count ? p.counts[n.count] : 0;
            return (
              <Link key={n.href} href={n.href} aria-current={on ? "page" : undefined} className={`m-nav${on ? " on" : ""}`}>
                <span className="m-nav-icon">
                  <Icon name={n.icon} size={17} />
                </span>
                <span style={{ flexGrow: 1 }}>{n.label}</span>
                {c ? <span className={`m-nav-count${n.red ? " red" : ""}`}>{c}</span> : null}
              </Link>
            );
          })}
        </nav>

        <div style={{ display: "flex", flexDirection: "column", gap: 2, marginTop: 10, paddingTop: 20, borderTop: "1px solid var(--line)" }}>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "0 10px 10px" }}>
            <span style={{ font: "500 12px/16px var(--font-mono)", letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--ink-faint)" }}>My sessions</span>
            <span style={{ display: "inline-flex", alignItems: "center", gap: 6, font: "11.5px var(--font-mono)", color: "var(--ink-faint)" }}>
              <span className={`m-dot${p.synced ? "" : " off"}`} />
              {p.synced ?? "no machine yet"}
            </span>
          </div>
          {p.sessions.length === 0 ? (
            <div style={{ margin: "2px 4px", padding: "16px 14px", border: "1px dashed var(--line-strong)", borderRadius: "var(--radius-lg)", display: "flex", flexDirection: "column", gap: 6 }}>
              <span style={{ fontSize: 13.5, fontWeight: 500 }}>No sessions yet</span>
              <span style={{ fontSize: 12.5, lineHeight: "18px", color: "var(--ink-faint)" }}>
                {p.connected ? "Start a Claude Code session. It shows up here after the next sync." : "Connect a machine and they show up here."}
              </span>
            </div>
          ) : (
            p.sessions.map((s) => {
              const on = path === `/sessions/${s.id}`;
              return (
                <Link key={s.id} href={`/sessions/${s.id}`} aria-current={on ? "page" : undefined} className={`m-nav m-nav-session${on ? " on" : ""}`}>
                  <SessionAvatar id={s.id} agent={s.agent} size={28} />
                  <span style={{ display: "flex", flexDirection: "column", flexGrow: 1, minWidth: 0 }}>
                    <span style={{ fontSize: 13, color: "var(--ink)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{s.title}</span>
                    <span style={{ font: "11.5px var(--font-mono)", color: "var(--ink-faint)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{s.meta}</span>
                  </span>
                  {s.open ? <span className="m-dot red" role="img" aria-label="Left something open to anyone" /> : null}
                </Link>
              );
            })
          )}
        </div>
      </div>
    </aside>
  );
}
