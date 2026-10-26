"use client";

import Link from "next/link";
import { useEffect, useRef, useState, type ReactNode } from "react";
import type { Member } from "@/contract";
import { api, ApiError, type OrgRef } from "@/lib/api";
import type { LiveStatus } from "@/lib/events";
import { CopyButton, Icon, SampleBadge, Segmented, Wordmark } from "../ui";
import type { Scope } from "./board";

export function LiveIndicator({ status }: { status: LiveStatus }) {
  const label =
    status === "live" ? "Live" : status === "connecting" ? "Connecting" : status === "reconnecting" ? "Reconnecting" : "Not live";
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-muted" role="status">
      <span
        className={`size-1.5 rounded-full ${status === "live" ? "pulse-dot bg-accent" : status === "off" ? "bg-line-strong" : "bg-muted"}`}
        aria-hidden="true"
      />
      {label}
    </span>
  );
}

export function DashboardHeader({
  scope,
  onScope,
  orgs,
  orgId,
  onOrg,
  sample,
  status,
  user,
  onSignOut,
}: {
  scope: Scope;
  onScope: (s: Scope) => void;
  orgs: OrgRef[];
  orgId: string | null;
  onOrg: (id: string) => void;
  sample: boolean;
  status: ReactNode;
  user?: string;
  onSignOut?: () => void;
}) {
  const org = orgs.find((o) => o.id === orgId);
  const [inviteOpen, setInviteOpen] = useState(false);

  return (
    <header className="sticky top-0 z-20 border-b border-line bg-bg/85 backdrop-blur">
      <div className="mx-auto flex h-14 max-w-[1600px] items-center gap-2 px-4 sm:gap-4 sm:px-6">
        <Link href="/" className="rounded-md" aria-label="Muretto home">
          <Wordmark compact />
        </Link>
        <span className="hidden h-5 w-px bg-line sm:block" aria-hidden="true" />

        {orgs.length > 0 ? (
          <label className="relative inline-flex items-center">
            <span className="sr-only">Team</span>
            <select
              value={orgId ?? ""}
              onChange={(e) => onOrg(e.target.value)}
              className="h-8 appearance-none rounded-lg border border-line bg-panel py-0 pr-8 pl-3 text-[13px] font-medium hover:border-line-strong"
            >
              {orgs.map((o) => (
                <option key={o.id} value={o.id}>
                  {o.name}
                </option>
              ))}
            </select>
            <Icon name="chevron" className="pointer-events-none absolute right-2.5 size-3.5 rotate-90 text-faint" />
          </label>
        ) : sample ? null : (
          <Link href="/app/orgs/new" className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-dashed border-line-strong px-3 text-[13px] text-muted hover:text-fg">
            <Icon name="plus" className="size-3.5" /> Create a team
          </Link>
        )}

        <Segmented<Scope>
          label="Scope"
          value={scope}
          onChange={onScope}
          options={[
            { value: "me", label: "Me", hint: "Your own synced data (m)" },
            { value: "team", label: "Team", disabled: orgs.length === 0, hint: orgs.length ? "Everyone in the team (t)" : "Create or join a team first" },
          ]}
        />

        <div className="ml-auto flex items-center gap-3 sm:gap-4">
          {sample ? <SampleBadge /> : null}
          <span className="hidden md:inline-flex">{status}</span>
          {org?.role === "owner" ? (
            <button
              type="button"
              onClick={() => setInviteOpen(true)}
              className="hidden h-8 items-center gap-1.5 rounded-lg border border-line px-3 text-[13px] text-muted hover:border-line-strong hover:text-fg sm:inline-flex"
            >
              <Icon name="users" className="size-3.5" /> Invite
            </button>
          ) : null}
          {user ? (
            <span className="hidden items-center gap-2 text-[13px] text-muted lg:inline-flex">
              <span className="font-mono">{user}</span>
              {onSignOut ? (
                <button type="button" onClick={onSignOut} className="rounded px-1 text-faint underline-offset-2 hover:text-fg hover:underline">
                  Sign out
                </button>
              ) : null}
            </span>
          ) : null}
        </div>
      </div>
      {org && inviteOpen ? <InviteDialog org={org} sample={sample} onClose={() => setInviteOpen(false)} /> : null}
    </header>
  );
}

function InviteDialog({ org, sample, onClose }: { org: OrgRef; sample: boolean; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [state, setState] = useState<{ kind: "idle" | "busy" | "error"; url?: string; expires?: string; error?: string }>({
    kind: "idle",
  });
  const [members, setMembers] = useState<Member[] | null>(null);

  useEffect(() => {
    if (sample) return;
    const ac = new AbortController();
    api.org(org.id, ac.signal).then(
      (o) => setMembers(o.members),
      () => {},
    );
    return () => ac.abort();
  }, [org.id, sample]);

  async function create() {
    setState({ kind: "busy" });
    try {
      const { url, expires_at } = await api.createInvite(org.id);
      setState({ kind: "idle", url, expires: expires_at });
    } catch (e) {
      setState({ kind: "error", error: e instanceof ApiError ? e.message : "Couldn't create an invite link." });
    }
  }

  return (
    <dialog
      ref={(d) => {
        ref.current = d;
        if (d && !d.open) d.showModal();
      }}
      className="modal"
      aria-labelledby="invite-title"
      onClose={onClose}
    >
      <div className="p-6">
        <h2 id="invite-title" className="text-base font-semibold">
          Invite to {org.name}
        </h2>
        <p className="mt-1.5 text-sm text-muted">
          Each link works once and expires. Teammates sign in with GitHub, then see the team view. Each person&apos;s data stays
          theirs; the team view is the union.
        </p>
        {sample ? (
          <p className="mt-4 rounded-lg border border-dashed border-accent-line bg-accent-wash px-3 py-2 text-sm text-accent">
            Sample data: invite links are created by the Muretto server.
          </p>
        ) : state.url ? (
          <>
            <div className="mt-4 flex items-center gap-2 rounded-lg border border-line bg-sunken py-1 pr-1 pl-3">
              <code className="min-w-0 flex-1 truncate font-mono text-xs">{state.url}</code>
              <CopyButton text={state.url} variant="solid" />
            </div>
            <p className="mt-1.5 text-xs text-faint">
              Single use.
              {state.expires ? ` Expires ${new Date(state.expires).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })}.` : ""}
            </p>
          </>
        ) : (
          <button
            type="button"
            onClick={create}
            disabled={state.kind === "busy"}
            className="mt-4 inline-flex h-9 items-center rounded-lg bg-accent px-4 text-sm font-medium text-accent-ink hover:bg-accent-strong disabled:opacity-60"
          >
            {state.kind === "busy" ? "Creating…" : "Create invite link"}
          </button>
        )}
        {state.kind === "error" ? (
          <p role="alert" className="mt-3 text-sm text-fg">
            {state.error}
          </p>
        ) : null}
        {members && members.length > 0 ? (
          <div className="mt-5">
            <h3 className="text-xs font-medium text-muted">Members</h3>
            <ul className="mt-1.5 divide-y divide-line rounded-lg border border-line text-sm">
              {members.map((m) => (
                <li key={m.developer_id} className="flex items-center justify-between gap-3 px-3 py-1.5">
                  <span className="min-w-0 truncate">
                    {m.display_name || m.account_login} <span className="font-mono text-xs text-faint">{m.account_login}</span>
                  </span>
                  <span className="text-xs text-faint">{m.role}</span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
        <div className="mt-6 flex justify-end">
          <button
            type="button"
            onClick={() => ref.current?.close()}
            className="h-8 rounded-lg border border-line px-3 text-[13px] text-muted hover:text-fg"
          >
            Done
          </button>
        </div>
      </div>
    </dialog>
  );
}
