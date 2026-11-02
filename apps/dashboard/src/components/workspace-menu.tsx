"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { UserAvatar } from "@/components/user-avatar";

/** The workspace switcher: the signed-in user, teams (soon), settings, sign out. */
export function WorkspaceMenu({ name, login, initials, avatarUrl }: { name: string; login: string; initials: string; avatarUrl?: string }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const item = { display: "flex", alignItems: "center", minHeight: 38, padding: "0 10px", borderRadius: "var(--radius-md)", fontSize: 14, color: "var(--ink-muted)", background: "transparent", border: 0, cursor: "pointer", width: "100%", textAlign: "left" as const };

  return (
    <div ref={ref} style={{ position: "relative" }}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-haspopup="menu"
        className="m-nav"
        style={{ display: "flex", alignItems: "center", gap: 10, width: "100%", minHeight: 56, padding: "8px 10px", border: "1px solid var(--line)", background: "var(--surface-raised)", borderRadius: "var(--radius-lg)", cursor: "pointer", textAlign: "left", color: "var(--ink)" }}
      >
        <UserAvatar initials={initials} url={avatarUrl} size={34} />
        <span style={{ display: "flex", flexDirection: "column", gap: 1, flexGrow: 1, minWidth: 0 }}>
          <span style={{ fontSize: 14, fontWeight: 600, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{name}</span>
          <span style={{ fontSize: 12.5, color: "var(--ink-faint)" }}>Solo workspace</span>
        </span>
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="var(--ink-faint)" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
          <path d="m7 15 5 5 5-5M7 9l5-5 5 5" />
        </svg>
      </button>
      {open ? (
        <div role="menu" style={{ position: "absolute", top: "calc(100% + 6px)", left: 0, right: 0, zIndex: 20, boxSizing: "border-box", padding: 6, border: "1px solid var(--line-strong)", borderRadius: "var(--radius-xl)", background: "var(--surface-raised)", boxShadow: "var(--shadow-pop)", display: "flex", flexDirection: "column", gap: 2 }}>
          <span style={{ padding: "8px 10px 6px", font: "12px var(--font-mono)", color: "var(--ink-faint)" }}>Signed in as @{login}</span>
          <Link role="menuitem" href="/" onClick={() => setOpen(false)} className="m-nav" style={{ display: "flex", alignItems: "center", gap: 10, minHeight: 48, padding: "6px 10px", borderRadius: "var(--radius-md)", color: "var(--ink)" }}>
            <UserAvatar initials={initials} url={avatarUrl} size={30} />
            <span style={{ display: "flex", flexDirection: "column", gap: 1, flexGrow: 1, minWidth: 0 }}>
              <span style={{ fontSize: 14, fontWeight: 500 }}>{name}</span>
              <span style={{ fontSize: 12.5, color: "var(--ink-faint)" }}>Solo workspace</span>
            </span>
            <span style={{ fontSize: 14, color: "var(--accent)" }}>✓</span>
          </Link>
          <div role="menuitem" aria-disabled="true" style={{ display: "flex", alignItems: "center", gap: 10, minHeight: 48, padding: "6px 10px", cursor: "default" }}>
            <span style={{ flex: "0 0 30px", height: 30, borderRadius: 8, border: "1px dashed var(--line-strong)", boxSizing: "border-box", display: "inline-flex", alignItems: "center", justifyContent: "center", color: "var(--ink-faint)" }}>
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                <circle cx="9" cy="8" r="3.5" />
                <path d="M2.5 20c.6-3.5 3.3-5.5 6.5-5.5s5.9 2 6.5 5.5M15.5 4.8a3.5 3.5 0 0 1 0 6.4M18 14.8c1.9.7 3.2 2.5 3.5 5.2" />
              </svg>
            </span>
            <span style={{ display: "flex", flexDirection: "column", gap: 1 }}>
              <span style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 14, fontWeight: 500, color: "var(--ink-muted)" }}>
                Teams <span className="m-soon">soon</span>
              </span>
              <span style={{ fontSize: 12.5, color: "var(--ink-faint)" }}>One view of everyone&apos;s sessions</span>
            </span>
          </div>
          <span style={{ height: 1, margin: "4px 6px", background: "var(--line)" }} />
          <Link role="menuitem" href="/settings" onClick={() => setOpen(false)} className="m-nav" style={item}>
            Settings
          </Link>
          <form action="/auth/signout" method="post">
            <button role="menuitem" type="submit" className="m-nav" style={item}>
              Sign out
            </button>
          </form>
        </div>
      ) : null}
    </div>
  );
}
