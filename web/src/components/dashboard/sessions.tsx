"use client";

import { useState } from "react";
import type { SessionView } from "@/contract";
import { kindLabel, plural, providerLabel, sessionSpan, shortId } from "@/lib/format";
import { groupResources, summarizeCosts, worstExposedName, type ResourceGroup } from "@/lib/model";
import { useClock } from "../clock";
import { Icon, Panel } from "../ui";
import { CostInline, DataAge, ResourceLine, useDash } from "./parts";

function GroupHeader({ g }: { g: ResourceGroup }) {
  return (
    <div className="flex items-baseline gap-3 px-2 pt-2 pb-1 font-mono text-[11px]">
      <span className="font-medium tracking-[0.08em] text-muted uppercase">{providerLabel(g.provider)}</span>
      {g.parent ? (
        <span className="text-faint">
          {kindLabel(g.parent.kind)} <span className="text-muted">{g.parent.name}</span>
        </span>
      ) : null}
    </div>
  );
}

export function ResourceGroups({ ids }: { ids: string[] }) {
  const { view } = useDash();
  const groups = groupResources(view, ids);
  if (groups.length === 0) {
    return <p className="px-2 py-1 text-xs text-faint">No resources from this session exist in connected accounts.</p>;
  }
  return (
    <div className="flex flex-col gap-1">
      {groups.map((g) => (
        <div key={`${g.provider}|${g.parentId ?? ""}`}>
          <GroupHeader g={g} />
          <ul>
            {g.resources.map((r) => (
              <ResourceLine key={r.id} r={r} />
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}

function SessionRow({ s, expanded, onToggle }: { s: SessionView; expanded: boolean; onToggle: () => void }) {
  const { view } = useDash();
  const { now, timeZone } = useClock();
  const running = !s.session.ended_at;
  const bodyId = `session-${s.session.id}`;
  const cost = summarizeCosts(view, s.resource_ids);
  const transcriptOnly = !s.session.sources.includes("hook");
  const exposedName = worstExposedName(view, s.resource_ids);

  return (
    <li className="border-b border-line last:border-b-0">
      <button
        type="button"
        aria-expanded={expanded}
        aria-controls={bodyId}
        onClick={onToggle}
        className="grid w-full grid-cols-[14px_minmax(0,1fr)_auto] gap-x-3 px-4 py-3 text-left hover:bg-hover"
      >
        <Icon
          name="chevron"
          className={`mt-[3px] size-3.5 text-faint transition-transform ${expanded ? "rotate-90" : ""}`}
        />
        <span className="flex min-w-0 flex-col gap-1">
          <span className="flex min-w-0 flex-wrap items-center gap-x-2 text-[13px]">
            <span className="font-mono font-medium text-accent">{shortId(s.session.id)}</span>
            <span className="font-medium">{s.developer_name}</span>
            <span className="text-faint">·</span>
            <span className="text-muted">Claude Code</span>
            <span className="text-faint">·</span>
            <span className="tabular text-muted">{sessionSpan(s.session.started_at, s.session.ended_at, now, timeZone)}</span>
            {running ? (
              <span className="inline-flex items-center gap-1 text-xs text-accent">
                <span className="pulse-dot size-1.5 rounded-full bg-accent" aria-hidden="true" />
                running
              </span>
            ) : null}
          </span>
          <span className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-xs">
            <span className="min-w-0 truncate text-fg">
              {s.session.task_title ? `“${s.session.task_title}”` : <span className="text-faint">no task title</span>}
            </span>
            {s.session.repo?.display_name ? (
              <span className="font-mono text-faint">
                {s.session.repo.display_name}
                {s.session.git_branch ? ` (${s.session.git_branch})` : ""}
              </span>
            ) : null}
            {transcriptOnly ? (
              <span
                className="rounded border border-dashed border-line-strong px-1 text-2xs text-muted"
                title="Seen only in the transcript: hooks didn't run for this session."
              >
                transcript only
              </span>
            ) : null}
          </span>
        </span>
        <span className="flex flex-col items-end gap-1 text-xs">
          <span className="flex items-center gap-1.5">
            {s.exposed_count > 0 ? (
              <span className="rounded-full bg-danger-wash px-2 py-0.5 font-medium text-danger">
                {s.exposed_count} exposed
              </span>
            ) : null}
            {s.review_due_count > 0 ? (
              <span className="rounded-full border border-accent-line px-2 py-0.5 text-accent">
                {s.review_due_count} review
              </span>
            ) : null}
            <span className="tabular text-muted">{plural(s.resource_ids.length, "resource")}</span>
          </span>
          <span className="hidden text-muted md:inline">
            <CostInline cost={cost} />
          </span>
        </span>
      </button>
      <div id={bodyId} hidden={!expanded} className="border-t border-dashed border-line bg-sunken/60 px-4 pt-1 pb-3 sm:pl-[42px]">
        <ResourceGroups ids={s.resource_ids} />
        <p className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 px-2 font-mono text-[11.5px] text-faint">
          <span className={s.exposed_count > 0 ? "text-danger" : ""}>{s.exposed_count} exposed</span>
          <span>·</span>
          <span>{s.review_due_count} needs review</span>
          {exposedName ? (
            <>
              <span className="ml-2">→ muretto why {exposedName}</span>
              <span>→ muretto fix {exposedName}</span>
            </>
          ) : null}
        </p>
      </div>
    </li>
  );
}

export function SessionsPanel({ className = "" }: { className?: string }) {
  const { view } = useDash();
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    const first = view.sessions.find((s) => s.exposed_count > 0) ?? view.sessions[0];
    return new Set(first ? [first.session.id] : []);
  });
  const [unknownOpen, setUnknownOpen] = useState(false);

  const toggle = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const unknown = view.unknownCreator;

  return (
    <Panel
      id="sessions"
      className={className}
      title={
        <>
          Sessions
          <span className="font-normal text-faint">git log for infrastructure</span>
        </>
      }
      aside={<DataAge at={view.checkedAt} />}
    >
      {view.sessions.length === 0 ? (
        <p className="px-4 py-6 text-sm text-muted">No agent sessions synced yet.</p>
      ) : (
        <ul>
          {view.sessions.map((s) => (
            <SessionRow
              key={s.session.id}
              s={s}
              expanded={expanded.has(s.session.id)}
              onToggle={() => toggle(s.session.id)}
            />
          ))}
        </ul>
      )}
      {unknown.length > 0 ? (
        <div className="border-t border-line">
          <button
            type="button"
            aria-expanded={unknownOpen}
            aria-controls="session-unknown"
            onClick={() => setUnknownOpen((v) => !v)}
            className="grid w-full grid-cols-[14px_minmax(0,1fr)_auto] items-center gap-x-3 px-4 py-3 text-left hover:bg-hover"
          >
            <Icon name="chevron" className={`size-3.5 text-faint transition-transform ${unknownOpen ? "rotate-90" : ""}`} />
            <span className="text-[13px]">
              <span className="font-medium">Creator unknown</span>{" "}
              <span className="text-muted">no recorded agent session created these. Nothing is hidden.</span>
            </span>
            <span className="tabular text-xs text-muted">{plural(unknown.length, "resource")}</span>
          </button>
          <div id="session-unknown" hidden={!unknownOpen} className="border-t border-dashed border-line bg-sunken/60 px-4 pt-1 pb-3 sm:pl-[42px]">
            <ResourceGroups ids={unknown.map((r) => r.id)} />
          </div>
        </div>
      ) : null}
    </Panel>
  );
}
