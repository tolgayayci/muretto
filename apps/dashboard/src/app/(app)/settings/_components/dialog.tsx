"use client";

// The two settings confirm dialogs: sign one machine out, sign out of the
// dashboard. The page opens them with ?confirm=<machine id> or ?confirm=signout
// (checked against the real machine ids on the server). This component stays
// mounted so the "signed out" view survives the refresh that removes the machine.
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState, useTransition, type ReactNode } from "react";
import { signOutMachineAction } from "@/app/(app)/actions";
import { Icon, type IconName } from "@/components/ds";
import { UserAvatar } from "@/components/user-avatar";

export type Confirm =
  | { kind: "machine"; id: string; name: string; facts: [string, string][] }
  | { kind: "signout"; name: string; login: string; initials: string; avatarUrl?: string };

const title = { margin: 0, fontSize: 30, lineHeight: "34px", fontWeight: 600, letterSpacing: "-0.03em", textWrap: "balance" } as const;

export function SettingsDialog({ confirm }: { confirm: Confirm | null }) {
  const router = useRouter();
  const [done, setDone] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const [pending, start] = useTransition();

  const close = useCallback(() => {
    setDone(null);
    setFailed(false);
    router.replace("/settings", { scroll: false });
  }, [router]);

  if (done === null && confirm === null) return null;

  if (done !== null) {
    return (
      <Modal key="done" onClose={close} label="dlg-h">
        <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
          <span className="m-tile ok" style={{ width: 44, height: 44, borderRadius: 12 }}>
            <Icon name="check-circle" size={20} />
          </span>
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            <h1 id="dlg-h" style={title}>
              <span style={{ display: "block" }}>{done} is signed out.</span>
              <span style={{ display: "block", color: "var(--ink-faint)" }}>Its sessions stay here.</span>
            </h1>
            <span style={{ fontSize: 15, lineHeight: "23px", color: "var(--ink-muted)" }}>
              To sign it in again, run <span className="mono" style={{ color: "var(--ink)" }}>muretto login</span> on it.
            </span>
          </div>
          <div className="m-full">
            <button type="button" className="mu-btn mu-btn-primary" data-autofocus onClick={close}>
              Back to settings
            </button>
          </div>
        </div>
      </Modal>
    );
  }

  if (confirm?.kind === "machine") {
    const run = () => {
      setFailed(false);
      start(async () => {
        const ok = await signOutMachineAction(confirm.id);
        if (!ok) {
          setFailed(true);
          return;
        }
        setDone(confirm.name);
        router.replace("/settings", { scroll: false });
      });
    };
    return (
      <Modal key={confirm.id} onClose={pending ? undefined : close} label="dlg-h">
        <div style={{ display: "flex", flexDirection: "column", gap: 26 }}>
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            <p className="m-eyebrow">Sign a machine out</p>
            <h1 id="dlg-h" style={title}>
              <span style={{ display: "block" }}>Sign out {confirm.name}?</span>
              <span style={{ display: "block", color: "var(--ink-faint)" }}>You can sign it in again.</span>
            </h1>
          </div>
          <div style={{ display: "flex", flexDirection: "column" }}>
            {confirm.facts.map(([k, v]) => (
              <div key={k} style={{ display: "flex", justifyContent: "space-between", gap: 12, padding: "9px 0", borderTop: "1px solid var(--line)", font: "13px var(--font-mono)" }}>
                <span style={{ color: "var(--ink-faint)" }}>{k}</span>
                <span style={{ color: "var(--ink)", textAlign: "right", minWidth: 0, overflowWrap: "anywhere" }}>{v}</span>
              </div>
            ))}
          </div>
          <What
            items={[
              ["key", "Its CLI token stops working, so it stops syncing."],
              ["file", "Its sessions and decisions stay here."],
              ["lock", "Nothing changes on the machine or in your accounts."],
            ]}
          />
          {failed ? (
            <span role="alert" style={{ marginTop: -10, fontSize: 14, color: "var(--ink-muted)" }}>
              Couldn&apos;t sign it out. Nothing changed. Try again.
            </span>
          ) : null}
          <div className="m-half">
            <button type="button" className="mu-btn mu-btn-secondary" data-autofocus disabled={pending} onClick={close}>
              Cancel
            </button>
            <button type="button" className="mu-btn mu-btn-secondary" disabled={pending} onClick={run}>
              {pending ? "Signing out…" : "Sign it out"}
            </button>
          </div>
        </div>
      </Modal>
    );
  }

  if (confirm?.kind !== "signout") return null;
  return (
    <Modal key="signout" onClose={close} label="dlg-h">
      <div style={{ display: "flex", flexDirection: "column", gap: 26 }}>
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <p className="m-eyebrow">Sign out</p>
          <h1 id="dlg-h" style={title}>
            <span style={{ display: "block" }}>Sign out of the dashboard?</span>
            <span style={{ display: "block", color: "var(--ink-faint)" }}>Only on this browser.</span>
          </h1>
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 16px", border: "1px solid var(--line)", borderRadius: "var(--radius-lg)", background: "var(--surface-sunken)" }}>
          <UserAvatar initials={confirm.initials} url={confirm.avatarUrl} size={36} />
          <span style={{ display: "flex", flexDirection: "column", gap: 2, flexGrow: 1, minWidth: 0 }}>
            <span style={{ fontSize: 15, fontWeight: 600, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{confirm.name}</span>
            <span style={{ font: "12.5px var(--font-mono)", color: "var(--ink-faint)" }}>@{confirm.login}</span>
          </span>
          <span style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)", whiteSpace: "nowrap" }}>this browser</span>
        </div>
        <What
          items={[
            ["laptop", "Your machines keep syncing. Sign them out one by one in Settings."],
            ["file", "Your sessions and decisions stay."],
            ["lock", "Sign back in with GitHub any time."],
          ]}
        />
        <form action="/auth/signout" method="post" className="m-half">
          <button type="button" className="mu-btn mu-btn-secondary" data-autofocus onClick={close}>
            Cancel
          </button>
          <button type="submit" className="mu-btn mu-btn-secondary">
            Sign out
          </button>
        </form>
      </div>
    </Modal>
  );
}

function What({ items }: { items: [IconName, string][] }) {
  return (
    <ul style={{ margin: 0, padding: 0, listStyle: "none", display: "flex", flexDirection: "column", gap: 12 }}>
      {items.map(([icon, text]) => (
        <li key={text} style={{ display: "flex", alignItems: "flex-start", gap: 12, fontSize: 14.5, lineHeight: "21px", color: "var(--ink-muted)" }}>
          <span className="m-tile" style={{ width: 28, height: 28, borderRadius: 8 }}>
            <Icon name={icon} size={14} />
          </span>
          <span style={{ paddingTop: 3 }}>{text}</span>
        </li>
      ))}
    </ul>
  );
}

/** Centered over a dim backdrop. Focuses the safe button, keeps Tab inside, Escape closes. */
function Modal({ onClose, label, children }: { onClose?: () => void; label: string; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const closeRef = useRef(onClose);
  useEffect(() => {
    closeRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    ref.current?.querySelector<HTMLElement>("[data-autofocus]")?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        closeRef.current?.();
        return;
      }
      if (e.key !== "Tab" || !ref.current) return;
      const items = [...ref.current.querySelectorAll<HTMLElement>("button:not([disabled]), a[href], input, [tabindex]:not([tabindex='-1'])")];
      if (!items.length) return;
      const first = items[0];
      const last = items[items.length - 1];
      if (e.shiftKey && (document.activeElement === first || !ref.current.contains(document.activeElement))) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && (document.activeElement === last || !ref.current.contains(document.activeElement))) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKey);
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = overflow;
      if (before?.isConnected) before.focus();
    };
  }, []);

  return (
    <div
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) closeRef.current?.();
      }}
      style={{ position: "fixed", inset: 0, zIndex: 50, background: "rgba(6, 6, 5, 0.76)", display: "flex", alignItems: "center", justifyContent: "center", padding: 24, boxSizing: "border-box", overflowY: "auto" }}
    >
      <div ref={ref} role="dialog" aria-modal="true" aria-labelledby={label} style={{ width: "100%", maxWidth: 440, boxSizing: "border-box", padding: 36, border: "1px solid var(--line-strong)", borderRadius: "var(--radius-2xl)", background: "var(--surface-raised)", boxShadow: "var(--shadow-float)" }}>
        {children}
      </div>
    </div>
  );
}
