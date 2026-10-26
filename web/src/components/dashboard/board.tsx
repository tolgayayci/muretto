"use client";

import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import type { Answer } from "@/contract";
import { buildView } from "@/lib/model";
import { CopyButton, Kbd } from "../ui";
import { ResourceDrawer } from "./drawer";
import { CostPanel, LiveFeed, ReviewQueue, RiskPanel, SummaryStrip, Warnings } from "./panels";
import { Dash, type DashContext } from "./parts";
import { SessionsPanel } from "./sessions";

export type Scope = "me" | "team";

/**
 * The one main dashboard screen, rendered from an Answer. Used by the live
 * dashboard, the sample-data dashboard and the landing-page preview.
 */
export function Board({
  answer,
  scope,
  sample,
  feedStatus,
  hiddenFeed,
  onScope,
  keyboard = true,
}: {
  answer: Answer;
  scope: Scope;
  sample: boolean;
  feedStatus: ReactNode;
  hiddenFeed?: Set<string>;
  onScope?: (s: Scope) => void;
  keyboard?: boolean;
}) {
  const view = useMemo(() => buildView(answer), [answer]);
  const [openId, setOpenId] = useState<string | null>(null);
  const [focus, setFocus] = useState<"exposure" | undefined>(undefined);

  const open = useCallback((id: string, f?: "exposure") => {
    setFocus(f);
    setOpenId(id);
  }, []);
  const ctx: DashContext = useMemo(() => ({ view, sample, open }), [view, sample, open]);

  useEffect(() => {
    if (!keyboard || !onScope) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName))) return;
      if (document.querySelector("dialog[open]")) return;
      if (e.key === "m") onScope("me");
      else if (e.key === "t") onScope("team");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [keyboard, onScope]);

  const empty = answer.sessions.length === 0 && answer.resources.length === 0;

  return (
    <Dash.Provider value={ctx}>
      {empty ? (
        <EmptyState scope={scope} />
      ) : (
        <div className="flex flex-col gap-4">
          <SummaryStrip />
          <Warnings />
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
            <div className="flex min-w-0 flex-col gap-4">
              <SessionsPanel key={scope} />
              <CostPanel scope={scope} />
            </div>
            <div className="flex min-w-0 flex-col gap-4">
              <RiskPanel />
              <LiveFeed hidden={hiddenFeed} status={feedStatus} />
              <ReviewQueue />
            </div>
          </div>
          {keyboard ? (
            <p className="flex flex-wrap items-center gap-x-4 gap-y-1 px-1 text-xs text-faint">
              <span className="inline-flex items-center gap-1.5">
                <Kbd>m</Kbd> me
              </span>
              <span className="inline-flex items-center gap-1.5">
                <Kbd>t</Kbd> team
              </span>
              <span className="inline-flex items-center gap-1.5">
                <Kbd>tab</Kbd> move · <Kbd>enter</Kbd> open · <Kbd>esc</Kbd> close
              </span>
              <span className="ml-auto">Read-only. Muretto never changes your infrastructure.</span>
            </p>
          ) : null}
        </div>
      )}
      <ResourceDrawer resourceId={openId} focus={focus} onClose={() => setOpenId(null)} />
    </Dash.Provider>
  );
}

function CommandStep({ n, cmd, text }: { n: number; cmd: string; text: string }) {
  return (
    <li className="grid grid-cols-[28px_minmax(0,1fr)] gap-3">
      <span className="tabular flex size-7 items-center justify-center rounded-full border border-line-strong font-mono text-xs text-muted">
        {n}
      </span>
      <div>
        <div className="flex items-center justify-between gap-2 rounded-lg border border-line bg-sunken py-1 pr-1 pl-3 font-mono text-[13px]">
          <span>
            <span className="text-faint">$ </span>
            {cmd}
          </span>
          <CopyButton text={cmd} />
        </div>
        <p className="mt-1.5 text-xs text-muted">{text}</p>
      </div>
    </li>
  );
}

export function EmptyState({ scope }: { scope: Scope }) {
  return (
    <section className="mx-auto mt-10 w-full max-w-xl rounded-2xl border border-line bg-panel p-8 shadow-panel">
      <h2 className="text-lg font-semibold tracking-[-0.01em]">
        {scope === "team" ? "Nothing synced to this team yet" : "Nothing synced yet"}
      </h2>
      <p className="mt-2 text-sm text-muted">
        Muretto runs on your machine first. Install it, connect Supabase and Vercel with read-only tokens, then sign the CLI
        in to sync here.
      </p>
      <ol className="mt-6 flex flex-col gap-4">
        <CommandStep n={1} cmd="npx muretto init" text="Adds Claude Code hooks (with a backup) and connects your providers." />
        <CommandStep n={2} cmd="muretto login" text="Approve the code on the device page of this dashboard." />
        <CommandStep n={3} cmd="muretto sync on" text="Shows exactly which fields will be sent before anything leaves your machine." />
      </ol>
    </section>
  );
}
