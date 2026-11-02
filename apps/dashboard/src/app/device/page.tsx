import type { Metadata } from "next";
import Link from "next/link";
import { Icon, Mark } from "@/components/ds";
import { lookupDevice } from "@/lib/api";
import { initials, requireViewer } from "@/lib/auth";
import { minutesUntil, normalizeCode } from "@/lib/device-code";
import { decideDeviceAction, enterCodeAction } from "./actions";

export const metadata: Metadata = { title: "Approve CLI sign-in" };
export const dynamic = "force-dynamic";

type Result = "approved" | "denied" | "expired" | "invalid" | "error";

export default async function DevicePage({ searchParams }: { searchParams: Promise<{ code?: string; result?: string }> }) {
  const sp = await searchParams;
  const viewer = await requireViewer();
  const result = (["approved", "denied", "expired", "invalid", "error"] as const).find((r) => r === sp.result) as Result | undefined;
  const code = normalizeCode(sp.code);
  const device = code && !result ? await lookupDevice(viewer, code) : null;

  return (
    <div style={{ minHeight: "100vh", boxSizing: "border-box", padding: "32px 48px", display: "flex", flexDirection: "column" }}>
      <header style={{ height: 40, display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <Link href="/" aria-label="muretto home">
          <Mark size={26} wordmark />
        </Link>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 10, fontSize: 14, color: "var(--ink-muted)" }}>
          <span style={{ width: 28, height: 28, borderRadius: 999, background: "var(--surface-hover)", border: "1px solid var(--line-strong)", boxSizing: "border-box", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 11, fontWeight: 600, color: "var(--ink)" }}>
            {initials(viewer.user.name)}
          </span>
          {viewer.user.login}
        </span>
      </header>

      <main style={{ flexGrow: 1, display: "flex", alignItems: "center", justifyContent: "center", padding: "40px 0 80px" }}>
        <div className="m-card" style={{ width: "100%", maxWidth: 440, padding: 36 }}>
          {result === "approved" ? (
            <Done
              icon="check-circle"
              title="The CLI is signed in."
              quiet="Back to your terminal."
              text={
                <>
                  Sync stays off until you run <span className="mono" style={{ color: "var(--ink)" }}>muretto sync on</span>. It lists every field first.
                </>
              }
              action={{ href: "/", label: "Open the dashboard", primary: true }}
            />
          ) : result === "denied" ? (
            <Done title="Sign-in denied." quiet="That code can't be used now." text="Run muretto login again if it was you." action={{ href: "/device", label: "Enter another code" }} />
          ) : result === "expired" || (code && !device && !result) ? (
            <Done title="This code has expired." quiet="Or it was already used." text="Codes last 15 minutes and work once. Run muretto login again for a new one." action={{ href: "/device", label: "Enter another code" }} />
          ) : result === "error" ? (
            <Done title="That didn't go through." quiet="Nothing was approved." text="Muretto's API didn't answer. Try again in a moment." action={{ href: code ? `/device?code=${code}` : "/device", label: "Try again", primary: true }} />
          ) : device && code ? (
            <Pending code={code} device={device} />
          ) : (
            <EnterCode invalid={result === "invalid" || (!!sp.code && !code)} />
          )}
        </div>
      </main>
    </div>
  );
}

function Pending({ code, device }: { code: string; device: Awaited<ReturnType<typeof lookupDevice>> & object }) {
  const letters = code.replace("-", "").split("");
  const c = device.client ?? {};
  const facts = [
    c.host ? { k: "machine", v: c.host } : null,
    c.os ? { k: "system", v: c.os } : null,
    { k: "cli", v: [c.client || "muretto", c.version].filter(Boolean).join(" ") },
    { k: "code expires", v: `in ${minutesUntil(device.expires_at)} min` },
  ].filter((f): f is { k: string; v: string } => !!f);
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 28 }}>
      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <p className="m-eyebrow">CLI sign-in</p>
        <h1 className="m-title" style={{ fontSize: 30, lineHeight: "34px" }}>
          <span>Is this your terminal?</span>
          <span className="q">Match the code first.</span>
        </h1>
      </div>
      <div role="group" aria-label={`Code ${code}`} style={{ display: "flex", alignItems: "center", gap: 6 }}>
        {letters.map((ch, i) => (
          <span key={i} style={{ display: "contents" }}>
            {i === 4 ? <span style={{ flex: "0 0 10px", height: 2, background: "var(--line-strong)" }} /> : null}
            <span style={{ flex: "1 1 0", height: 56, borderRadius: "var(--radius-md)", border: "1px solid var(--line-strong)", background: "var(--surface-sunken)", display: "inline-flex", alignItems: "center", justifyContent: "center", font: "500 24px var(--font-mono)" }}>{ch}</span>
          </span>
        ))}
      </div>
      <div style={{ display: "flex", flexDirection: "column" }}>
        {facts.map((f) => (
          <div key={f.k} style={{ display: "flex", justifyContent: "space-between", gap: 12, padding: "9px 0", borderTop: "1px solid var(--line)", font: "13px var(--font-mono)" }}>
            <span style={{ color: "var(--ink-faint)" }}>{f.k}</span>
            <span style={{ color: "var(--ink)", textAlign: "right", overflowWrap: "anywhere" }}>{f.v}</span>
          </div>
        ))}
      </div>
      <form action={decideDeviceAction} style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <input type="hidden" name="code" value={code} />
        <div className="m-half">
          <button type="submit" name="decision" value="deny" className="mu-btn mu-btn-secondary">
            Deny
          </button>
          <button type="submit" name="decision" value="approve" className="mu-btn mu-btn-primary">
            Approve
          </button>
        </div>
        <span style={{ fontSize: 14, color: "var(--ink-faint)", textAlign: "center" }}>
          Didn&apos;t just run <span className="mono" style={{ color: "var(--ink-muted)" }}>muretto login</span>? Deny it.
        </span>
      </form>
    </div>
  );
}

function EnterCode({ invalid }: { invalid: boolean }) {
  return (
    <form action={enterCodeAction} style={{ display: "flex", flexDirection: "column", gap: 24 }}>
      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <p className="m-eyebrow">CLI sign-in</p>
        <h1 className="m-title" style={{ fontSize: 30, lineHeight: "34px" }}>
          <span>Enter the code.</span>
          <span className="q">The one muretto login shows.</span>
        </h1>
      </div>
      <label style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <span style={{ fontSize: 14, color: "var(--ink-muted)" }}>Code</span>
        <input
          name="code"
          required
          autoFocus
          autoComplete="off"
          spellCheck={false}
          maxLength={9}
          placeholder="XXXX-XXXX"
          aria-invalid={invalid || undefined}
          style={{ height: 52, boxSizing: "border-box", padding: "0 16px", border: `1px solid ${invalid ? "var(--ink-muted)" : "var(--control-line)"}`, borderRadius: "var(--radius-md)", background: "var(--surface-sunken)", color: "var(--ink)", font: "500 22px var(--font-mono)", letterSpacing: "0.12em", textTransform: "uppercase" }}
        />
        {invalid ? (
          <span role="alert" style={{ fontSize: 13.5, color: "var(--ink-muted)" }}>
            That doesn&apos;t look like a Muretto code. It&apos;s 8 letters, like BKTW-HRZP.
          </span>
        ) : null}
      </label>
      <div className="m-full">
        <button type="submit" className="mu-btn mu-btn-primary">
          Continue
        </button>
      </div>
      <span style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13.5, color: "var(--ink-faint)" }}>
        <Icon name="terminal" size={15} /> Run <span className="mono" style={{ color: "var(--ink-muted)" }}>muretto login</span> in your terminal to get one.
      </span>
    </form>
  );
}

function Done({ icon, title, quiet, text, action }: { icon?: "check-circle"; title: string; quiet: string; text: React.ReactNode; action: { href: string; label: string; primary?: boolean } }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
      {icon ? <Icon name={icon} tile /> : null}
      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <h1 className="m-title" style={{ fontSize: 30, lineHeight: "34px" }}>
          <span>{title}</span>
          <span className="q">{quiet}</span>
        </h1>
        <span style={{ fontSize: 15, lineHeight: "23px", color: "var(--ink-muted)" }}>{text}</span>
      </div>
      <div className="m-full">
        <Link href={action.href} className={`mu-btn ${action.primary ? "mu-btn-primary" : "mu-btn-secondary"}`}>
          {action.label}
        </Link>
      </div>
    </div>
  );
}

