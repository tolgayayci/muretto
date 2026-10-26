"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { buttonPrimary } from "@/components/auth-card";
import { Icon } from "@/components/ui";
import { api, ApiError, rememberReturnTo } from "@/lib/api";

type State = { kind: "idle" | "busy" | "signin" } | { kind: "error"; message: string };

export function AcceptInvite({ token }: { token: string }) {
  const router = useRouter();
  const [state, setState] = useState<State>({ kind: "idle" });

  async function accept() {
    setState({ kind: "busy" });
    try {
      await api.acceptInvite(token);
      try {
        localStorage.setItem("muretto.scope", "team");
        localStorage.removeItem("muretto.org");
      } catch {
        /* fine */
      }
      router.push("/app");
    } catch (err) {
      if (err instanceof ApiError && err.kind === "unauthorized") {
        rememberReturnTo(`/join/${encodeURIComponent(token)}`);
        setState({ kind: "signin" });
      } else if (err instanceof ApiError && (err.kind === "invalid" || err.kind === "not_found")) {
        setState({ kind: "error", message: "This invite link is invalid, used or expired. Ask for a new one." });
      } else if (err instanceof ApiError && err.kind === "unreachable") {
        setState({ kind: "error", message: "Can't reach the Muretto server." });
      } else {
        setState({ kind: "error", message: err instanceof Error ? err.message : "Couldn't accept the invite." });
      }
    }
  }

  if (state.kind === "signin") {
    return (
      <div className="mt-4">
        <p>Sign in with GitHub first. We&apos;ll bring you back to this invite.</p>
        <a href={api.signInURL()} className={`${buttonPrimary} mt-5`}>
          <Icon name="github" /> Continue with GitHub
        </a>
      </div>
    );
  }

  return (
    <div className="mt-4">
      <p>
        You&apos;ve been invited to a team. You&apos;ll see the team view: the sessions, resources and exposures your teammates
        sync. Your own data is shared with the team only if you sync it.
      </p>
      {state.kind === "error" ? (
        <p role="alert" className="mt-3 flex items-center gap-1.5 text-sm text-fg">
          <Icon name="alert" className="size-4 text-muted" /> {state.message}
        </p>
      ) : null}
      <button type="button" onClick={accept} disabled={state.kind === "busy"} className={`${buttonPrimary} mt-5`}>
        {state.kind === "busy" ? "Joining…" : "Accept invite"}
      </button>
    </div>
  );
}
