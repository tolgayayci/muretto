"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { Icon } from "../ui";

type State = { kind: "idle" | "busy" | "done" } | { kind: "error"; message: string };

/** "Request a provider": posts to POST /v1/provider-requests. */
export function ProviderRequestForm() {
  const [provider, setProvider] = useState("");
  const [email, setEmail] = useState("");
  const [state, setState] = useState<State>({ kind: "idle" });

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const p = provider.trim();
    if (!p) {
      setState({ kind: "error", message: "Which provider should Muretto read next?" });
      return;
    }
    setState({ kind: "busy" });
    try {
      await api.requestProvider({ provider: p, ...(email.trim() ? { email: email.trim() } : {}) });
      setState({ kind: "done" });
    } catch (err) {
      const message =
        err instanceof ApiError && err.kind === "rate_limited"
          ? "Too many requests from here. Try again in a minute."
          : err instanceof ApiError && err.kind === "invalid"
            ? "That doesn't look right. Check the provider name and email."
            : err instanceof ApiError && err.kind === "unreachable"
              ? "Couldn't reach the Muretto server. Try again later, or open a GitHub issue."
              : "Something went wrong. Try again, or open a GitHub issue.";
      setState({ kind: "error", message });
    }
  }

  if (state.kind === "done") {
    return (
      <div role="status" className="flex min-h-[156px] flex-col justify-center rounded-xl border border-accent-line bg-accent-wash p-5">
        <p className="flex items-center gap-2 font-medium text-fg">
          <Icon name="check" className="size-5 text-accent" /> Request recorded: {provider.trim()}
        </p>
        <p className="mt-1 text-sm text-muted">Requests decide which adapter we write next.</p>
        <button
          type="button"
          onClick={() => {
            setProvider("");
            setState({ kind: "idle" });
          }}
          className="mt-3 self-start text-sm text-muted underline underline-offset-2 hover:text-fg"
        >
          Request another
        </button>
      </div>
    );
  }

  const input =
    "h-10 w-full rounded-lg border border-line-strong bg-sunken px-3 text-sm text-fg placeholder:text-faint focus-visible:border-accent";

  return (
    <form onSubmit={submit} noValidate className="flex min-h-[156px] flex-col gap-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <label htmlFor="req-provider" className="mb-1.5 block text-xs font-medium text-muted">
            Provider
          </label>
          <input
            id="req-provider"
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            maxLength={100}
            placeholder="Neon, Cloudflare, Railway…"
            required
            aria-invalid={state.kind === "error" && !provider.trim()}
            className={input}
          />
        </div>
        <div>
          <label htmlFor="req-email" className="mb-1.5 block text-xs font-medium text-muted">
            Email <span className="font-normal text-faint">(optional)</span>
          </label>
          <input
            id="req-email"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            maxLength={200}
            placeholder="you@example.com"
            autoComplete="email"
            className={input}
          />
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="submit"
          disabled={state.kind === "busy"}
          className="inline-flex h-10 items-center rounded-lg bg-accent px-4 text-sm font-medium text-accent-ink hover:bg-accent-strong disabled:opacity-60"
        >
          {state.kind === "busy" ? "Sending…" : "Request a provider"}
        </button>
        {state.kind === "error" ? (
          <p role="alert" className="flex items-center gap-1.5 text-sm text-fg">
            <Icon name="alert" className="size-4 shrink-0 text-muted" />
            {state.message}
          </p>
        ) : (
          <p className="text-xs text-faint">Email is optional.</p>
        )}
      </div>
    </form>
  );
}
