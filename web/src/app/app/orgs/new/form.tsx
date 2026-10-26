"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { buttonPrimary, inputClass } from "@/components/auth-card";
import { Icon } from "@/components/ui";
import { api, ApiError, rememberReturnTo } from "@/lib/api";

export function CreateOrgForm() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      setError("Give the team a name.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const org = await api.createOrg(trimmed);
      try {
        localStorage.setItem("muretto.org", org.id);
        localStorage.setItem("muretto.scope", "team");
      } catch {
        /* not remembered; the dashboard picks the first team */
      }
      router.push("/app");
    } catch (err) {
      setBusy(false);
      if (err instanceof ApiError && err.kind === "unauthorized") {
        rememberReturnTo("/app/orgs/new");
        router.push("/signin");
      } else {
        setError(err instanceof ApiError && err.kind === "unreachable" ? "Can't reach the Muretto server." : (err as Error).message);
      }
    }
  }

  return (
    <form onSubmit={submit} noValidate className="mt-4">
      <p>Then invite teammates with a single-use link.</p>
      <label htmlFor="org-name" className="mt-5 block text-xs font-medium text-muted">
        Team name
      </label>
      <input
        id="org-name"
        value={name}
        onChange={(e) => setName(e.target.value)}
        maxLength={100}
        placeholder="acme"
        autoComplete="organization"
        aria-invalid={!!error}
        aria-describedby={error ? "org-error" : undefined}
        className={`${inputClass} mt-1.5`}
      />
      {error ? (
        <p id="org-error" role="alert" className="mt-2 flex items-center gap-1.5 text-sm text-fg">
          <Icon name="alert" className="size-4 text-muted" /> {error}
        </p>
      ) : null}
      <button type="submit" disabled={busy} className={`${buttonPrimary} mt-5`}>
        {busy ? "Creating…" : "Create team"}
      </button>
    </form>
  );
}
