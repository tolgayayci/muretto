"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useState } from "react";
import { buttonPrimary, inputClass } from "@/components/auth-card";
import { Icon } from "@/components/ui";
import { api, ApiError, rememberReturnTo } from "@/lib/api";

type State =
  | { kind: "idle" }
  | { kind: "busy" }
  | { kind: "approved" }
  | { kind: "signin" }
  | { kind: "error"; message: string };

/** Codes look like WDJB-MJHT; accept any case and missing hyphen. */
function normalize(raw: string): string {
  const s = raw.toUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, 8);
  return s.length > 4 ? `${s.slice(0, 4)}-${s.slice(4)}` : s;
}

export function DeviceApproval() {
  const params = useSearchParams();
  const [code, setCode] = useState(() => normalize(params.get("code") ?? params.get("user_code") ?? ""));
  const [state, setState] = useState<State>({ kind: "idle" });

  async function approve(e: React.FormEvent) {
    e.preventDefault();
    if (code.replace("-", "").length < 8) {
      setState({ kind: "error", message: "Enter the 8-character code shown by muretto login." });
      return;
    }
    setState({ kind: "busy" });
    try {
      await api.approveDevice(code);
      setState({ kind: "approved" });
    } catch (err) {
      if (err instanceof ApiError && err.kind === "unauthorized") {
        rememberReturnTo(`/device?code=${encodeURIComponent(code)}`);
        setState({ kind: "signin" });
      } else if (err instanceof ApiError && (err.kind === "invalid" || err.kind === "not_found")) {
        setState({ kind: "error", message: "That code is invalid or has expired. Run muretto login again." });
      } else if (err instanceof ApiError && err.kind === "rate_limited") {
        setState({ kind: "error", message: "Too many attempts. Wait a minute and try again." });
      } else if (err instanceof ApiError && err.kind === "unreachable") {
        setState({ kind: "error", message: "Can't reach the Muretto server." });
      } else {
        setState({ kind: "error", message: err instanceof Error ? err.message : "Approval failed." });
      }
    }
  }

  if (state.kind === "approved") {
    return (
      <div role="status" className="mt-4">
        <p className="flex items-center gap-2 text-fg">
          <Icon name="check" className="size-5 text-accent" /> CLI login approved.
        </p>
        <p className="mt-2">
          Go back to your terminal. Then run <code className="font-mono text-fg">muretto sync on</code> to choose what to
          sync.
        </p>
        <Link href="/app" className="mt-5 inline-flex text-sm text-fg underline underline-offset-2">
          Open the dashboard
        </Link>
      </div>
    );
  }

  if (state.kind === "signin") {
    return (
      <div className="mt-4">
        <p>Sign in first, then approve the code. We&apos;ll bring you back here.</p>
        <a href={api.signInURL()} className={`${buttonPrimary} mt-5`}>
          <Icon name="github" /> Continue with GitHub
        </a>
      </div>
    );
  }

  return (
    <form onSubmit={approve} className="mt-4" noValidate>
      <p>Enter the code shown in your terminal by <code className="font-mono text-fg">muretto login</code>.</p>
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
        inputMode="text"
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
        {state.kind === "busy" ? "Approving…" : "Approve"}
      </button>
    </form>
  );
}
