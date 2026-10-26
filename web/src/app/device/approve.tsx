"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { buttonPrimary, inputClass } from "@/components/auth-card";
import { Icon } from "@/components/ui";
import { api, ApiError, type DeviceInfo } from "@/lib/api";

type State =
  | { kind: "enter" }
  | { kind: "busy" }
  | { kind: "confirm"; info: DeviceInfo }
  | { kind: "deciding"; info: DeviceInfo }
  | { kind: "done"; status: "approved" | "denied" }
  | { kind: "signin" }
  | { kind: "error"; message: string };

/** Codes look like WDJB-MJHT; accept any case and a missing hyphen. */
function normalize(raw: string): string {
  const s = raw.toUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, 8);
  return s.length > 4 ? `${s.slice(0, 4)}-${s.slice(4)}` : s;
}

function explain(err: unknown): State {
  if (err instanceof ApiError) {
    if (err.kind === "unauthorized") return { kind: "signin" };
    if (err.kind === "invalid" || err.kind === "not_found")
      return { kind: "error", message: "That code is invalid or has expired. Run muretto login again." };
    if (err.kind === "rate_limited") return { kind: "error", message: "Too many attempts. Wait a minute and try again." };
    if (err.kind === "unreachable") return { kind: "error", message: "Can't reach the Muretto server." };
  }
  return { kind: "error", message: err instanceof Error ? err.message : "Something went wrong." };
}

export function DeviceApproval() {
  const params = useSearchParams();
  const initial = normalize(params.get("code") ?? params.get("user_code") ?? "");
  const [code, setCode] = useState(initial);
  const [state, setState] = useState<State>(initial.length === 9 ? { kind: "busy" } : { kind: "enter" });

  // A complete code in the URL: look it up straight away.
  useEffect(() => {
    if (initial.length !== 9) return;
    const ac = new AbortController();
    api.device(initial, ac.signal).then(
      (info) => setState({ kind: "confirm", info }),
      (err: unknown) => {
        if (!ac.signal.aborted) setState(explain(err));
      },
    );
    return () => ac.abort();
  }, [initial]);

  async function lookup(e: React.FormEvent) {
    e.preventDefault();
    if (code.length !== 9) {
      setState({ kind: "error", message: "Enter the 8-character code shown by muretto login." });
      return;
    }
    setState({ kind: "busy" });
    try {
      setState({ kind: "confirm", info: await api.device(code) });
    } catch (err) {
      setState(explain(err));
    }
  }

  async function decide(info: DeviceInfo, deny: boolean) {
    setState({ kind: "deciding", info });
    try {
      const r = await api.decideDevice(info.user_code, deny);
      setState({ kind: "done", status: r.status });
    } catch (err) {
      setState(explain(err));
    }
  }

  if (state.kind === "done") {
    return state.status === "approved" ? (
      <div role="status" className="mt-4">
        <p className="flex items-center gap-2 text-fg">
          <Icon name="check" className="size-5 text-accent" /> CLI login approved.
        </p>
        <p className="mt-2">
          Go back to your terminal. Then run <code className="font-mono text-fg">muretto sync on</code> to see exactly what
          would be sent before anything is.
        </p>
        <Link href="/app" className="mt-5 inline-flex text-sm text-fg underline underline-offset-2">
          Open the dashboard
        </Link>
      </div>
    ) : (
      <p role="status" className="mt-4 flex items-center gap-2 text-fg">
        <Icon name="x" className="size-5 text-muted" /> Denied. That CLI won&apos;t get a token.
      </p>
    );
  }

  if (state.kind === "signin") {
    return (
      <div className="mt-4">
        <p>Sign in first. You&apos;ll come straight back to this code.</p>
        <a href={api.signInURL(`/device?code=${encodeURIComponent(code)}`)} className={`${buttonPrimary} mt-5`}>
          <Icon name="github" /> Continue with GitHub
        </a>
      </div>
    );
  }

  if (state.kind === "confirm" || state.kind === "deciding") {
    const c = state.info.client ?? {};
    const rows: [string, string | undefined][] = [
      ["Code", state.info.user_code],
      ["Computer", c.host],
      ["System", c.os],
      ["Client", [c.client, c.version].filter(Boolean).join(" ") || undefined],
    ];
    const busy = state.kind === "deciding";
    return (
      <div className="mt-4">
        <p>Is this the computer you ran muretto login on?</p>
        <dl className="mt-4 grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1.5 rounded-lg border border-line bg-sunken p-3 text-sm">
          {rows
            .filter(([, v]) => v)
            .map(([k, v]) => (
              <div key={k} className="contents">
                <dt className="text-faint">{k}</dt>
                <dd className="truncate font-mono text-fg">{v}</dd>
              </div>
            ))}
        </dl>
        <div className="mt-5 grid grid-cols-2 gap-3">
          <button
            type="button"
            disabled={busy}
            onClick={() => decide(state.info, true)}
            className="h-10 rounded-lg border border-line-strong text-sm font-medium hover:bg-hover disabled:opacity-60"
          >
            Deny
          </button>
          <button type="button" disabled={busy} onClick={() => decide(state.info, false)} className={buttonPrimary}>
            {busy ? "Saving…" : "Approve"}
          </button>
        </div>
      </div>
    );
  }

  return (
    <form onSubmit={lookup} className="mt-4" noValidate>
      <p>
        Enter the code shown in your terminal by <code className="font-mono text-fg">muretto login</code>.
      </p>
      <label htmlFor="user-code" className="mt-5 block text-xs font-medium text-muted">
        Code
      </label>
      <input
        id="user-code"
        name="user_code"
        value={code}
        onChange={(e) => setCode(normalize(e.target.value))}
        placeholder="XXXX-XXXX"
        autoComplete="one-time-code"
        autoCapitalize="characters"
        spellCheck={false}
        aria-invalid={state.kind === "error"}
        aria-describedby={state.kind === "error" ? "code-error" : undefined}
        className={`${inputClass} mt-1.5 h-12 text-center font-mono text-xl tracking-[0.3em]`}
      />
      {state.kind === "error" ? (
        <p id="code-error" role="alert" className="mt-2 flex items-center gap-1.5 text-sm text-fg">
          <Icon name="alert" className="size-4 shrink-0 text-muted" /> {state.message}
        </p>
      ) : null}
      <button type="submit" disabled={state.kind === "busy"} className={`${buttonPrimary} mt-5`}>
        {state.kind === "busy" ? "Checking…" : "Continue"}
      </button>
    </form>
  );
}
