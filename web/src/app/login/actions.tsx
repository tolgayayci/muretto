"use client";

import { useSearchParams } from "next/navigation";
import { useState } from "react";
import { buttonPrimary, inputClass } from "@/components/auth-card";
import { Icon } from "@/components/ui";
import { api, safePath } from "@/lib/api";

// Failure reasons the server redirects with: /login?error=state|denied|github|expired
const ERRORS: Record<string, string> = {
  state: "The sign-in request didn't match. Please try again.",
  denied: "GitHub sign-in was cancelled.",
  github: "GitHub didn't respond as expected. Please try again.",
  expired: "The sign-in took too long and expired. Please try again.",
};

export function SignInActions({ dev }: { dev: boolean }) {
  const params = useSearchParams();
  const returnTo = safePath(params.get("return_to"));
  const error = params.get("error");
  const [devLogin, setDevLogin] = useState("alice");

  return (
    <>
      {error ? (
        <p role="alert" className="mt-5 flex items-center gap-1.5 rounded-lg border border-line bg-sunken px-3 py-2 text-sm text-fg">
          <Icon name="alert" className="size-4 shrink-0 text-muted" />
          {ERRORS[error] ?? "Sign-in failed. Please try again."}
        </p>
      ) : null}
      <a href={api.signInURL(returnTo)} className={`${buttonPrimary} mt-6`}>
        <Icon name="github" /> Continue with GitHub
      </a>
      {dev ? (
        <form
          className="mt-5 rounded-lg border border-dashed border-line-strong p-3"
          onSubmit={(e) => {
            e.preventDefault();
            window.location.href = api.devSignInURL(devLogin.trim() || "alice", returnTo);
          }}
        >
          <label htmlFor="dev-login" className="block text-xs text-muted">
            Development only: sign in as any user on a local server started with{" "}
            <code className="font-mono text-fg">MURETTO_DEV_LOGIN=1</code>
          </label>
          <div className="mt-2 flex gap-2">
            <input id="dev-login" value={devLogin} onChange={(e) => setDevLogin(e.target.value)} className={`${inputClass} h-9 font-mono`} />
            <button type="submit" className="h-9 shrink-0 rounded-lg border border-line-strong px-3 text-sm hover:bg-hover">
              Dev sign-in
            </button>
          </div>
        </form>
      ) : null}
    </>
  );
}
