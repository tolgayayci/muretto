"use client";

import { useState } from "react";
import { daysUntil, kindLabel, plural, shortId, timeAgo } from "@/lib/format";
import { headlineFinding, summarizeCosts, type FeedItem } from "@/lib/model";
import { useClock } from "../clock";
import { AttributionLabel, Icon, Panel } from "../ui";
import { CostAmounts, DataAge, ExposureText, UnknownCost, useDash } from "./parts";

// ------------------------------------------------------------------ summary

function Stat({
  label,
  value,
  tone = "default",
  hint,
}: {
  label: string;
  value: React.ReactNode;
  tone?: "default" | "danger" | "accent";
  hint?: string;
}) {
  const color = tone === "danger" ? "text-danger" : tone === "accent" ? "text-accent" : "text-fg";
  return (
    <div className={`flex min-w-0 flex-col gap-1 px-4 py-3 ${tone === "danger" ? "bg-danger-wash" : ""}`} title={hint}>
      <dt className="truncate text-xs text-muted">{label}</dt>
      <dd className={`tabular text-[22px] leading-none font-medium tracking-[-0.02em] ${color}`}>{value}</dd>
    </div>
  );
}

export function SummaryStrip() {
  const { view } = useDash();
  const s = view.answer.summary;
  return (
    <dl className="grid grid-cols-2 divide-line overflow-hidden rounded-xl border border-line bg-panel shadow-panel sm:grid-cols-4 sm:divide-x lg:grid-cols-7">
      <Stat label="Sessions" value={s.sessions} />
      <Stat label="Agent-created" value={s.agent_created} hint="Resources with an exact attribution" />
      <Stat label="Still existing" value={s.still_existing} hint="Agent-created resources confirmed by the last check" />
      <Stat label="Exposed" value={s.exposed} tone={s.exposed > 0 ? "danger" : "default"} />
      <Stat label="Needs review" value={s.needs_review} tone={s.needs_review > 0 ? "accent" : "default"} hint="Pending and past due" />
      <Stat label="Creator unknown" value={s.unknown_creator} hint="No recorded agent session created these" />
      <Stat
        label="Provider checks"
        value={
          <span>
            {s.checks_ok}
            <span className="text-sm text-faint"> ok</span>
            {s.checks_failed > 0 ? (
              <>
                <span className="text-sm text-faint"> · </span>
                {s.checks_failed}
                <span className="text-sm text-faint"> failed</span>
              </>
            ) : null}
          </span>
        }
      />
    </dl>
  );
}

/** Renders `backticked` spans of server-provided text as inline code. Plain text otherwise. */
export function Inline({ text }: { text: string }) {
  return (
    <>
      {text.split(/(`[^`]+`)/g).map((part, i) =>
        part.startsWith("`") && part.endsWith("`") && part.length > 2 ? (
          <code key={i} className="font-mono text-[0.92em] text-fg">
            {part.slice(1, -1)}
          </code>
        ) : (
          part
        ),
      )}
    </>
  );
}

export function Warnings() {
  const { view } = useDash();
  if (view.answer.warnings.length === 0) return null;
  return (
    <ul className="flex flex-col gap-1">
      {view.answer.warnings.map((w) => (
        <li key={w.code} className="flex flex-wrap items-baseline gap-x-2 px-1 text-xs text-muted">
          <Icon name="question" className="size-3.5 translate-y-[2px] text-faint" />
          <span>
            <Inline text={w.message} />
          </span>
          {w.fix ? (
            <span className="text-faint">
              <Inline text={w.fix} />
            </span>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

// ------------------------------------------------------------------ live feed

function FeedRow({ item, fresh }: { item: FeedItem; fresh: boolean }) {
  const { view, open } = useDash();
  const { now } = useClock();
  const r = item.resource;
  const f = headlineFinding(view, r.id);
  return (
    <li className={fresh ? "feed-new" : ""}>
      <button
        type="button"
        onClick={() => open(r.id, f?.state === "exposed" ? "exposure" : undefined)}
        className="flex w-full flex-col gap-1 px-4 py-2.5 text-left hover:bg-hover"
      >
        <span className="flex w-full min-w-0 items-center gap-2 text-xs">
          {item.session ? (
            <>
              <span className="font-medium text-fg">{item.session.developer_name}</span>
              <span className="text-faint">·</span>
              <span className="font-mono text-accent">{shortId(item.session.session.id)}</span>
              <span className="text-faint">·</span>
              <span className="min-w-0 flex-1 truncate text-muted">{item.session.session.task_title ?? ""}</span>
            </>
          ) : (
            <span className="min-w-0 flex-1 truncate text-muted">no recorded session</span>
          )}
          <span className="tabular shrink-0 text-faint">{timeAgo(item.at, now)}</span>
        </span>
        <span className="flex min-w-0 items-center gap-2 font-mono text-[12.5px]">
          <span className="text-faint">→</span>
          <span className="text-faint">{kindLabel(r.kind)}</span>
          <span className="min-w-0 truncate">{r.name}</span>
          <AttributionLabel label={item.attribution?.label} />
          <span className="min-w-0 font-sans text-xs">
            <ExposureText finding={f} compact />
          </span>
        </span>
      </button>
    </li>
  );
}

export function LiveFeed({ hidden, status }: { hidden?: Set<string>; status: React.ReactNode }) {
  const { view } = useDash();
  const items = hidden ? view.feed.filter((i) => !hidden.has(i.key)) : view.feed;
  const keys = items.map((i) => i.key).join("|");

  // Rows that weren't in the previous render animate in. The first render doesn't.
  const [prevKeys, setPrevKeys] = useState(keys);
  const [fresh, setFresh] = useState<Set<string>>(new Set());
  if (prevKeys !== keys) {
    const before = new Set(prevKeys.split("|"));
    setFresh(new Set(items.map((i) => i.key).filter((k) => !before.has(k))));
    setPrevKeys(keys);
  }

  return (
    <Panel id="feed" title="Live feed" aside={status}>
      <ol aria-live="polite" aria-relevant="additions" className="scroll-thin max-h-[332px] divide-y divide-line overflow-y-auto">
        {items.length === 0 ? (
          <li className="px-4 py-6 text-sm text-muted">Resources appear here as agents create them.</li>
        ) : (
          items.map((i) => <FeedRow key={i.key} item={i} fresh={fresh.has(i.key)} />)
        )}
      </ol>
    </Panel>
  );
}

// ------------------------------------------------------------------ risk

export function RiskPanel() {
  const { view, open } = useDash();
  const { now } = useClock();
  const exposed = view.exposed;
  return (
    <Panel
      id="risk"
      tone={exposed.length > 0 ? "danger" : "default"}
      title={
        <>
          Exposed
          <span
            className={`tabular rounded-full px-1.5 text-xs ${exposed.length > 0 ? "bg-danger text-danger-ink" : "bg-hover text-muted"}`}
          >
            {exposed.length}
          </span>
        </>
      }
      aside={<DataAge at={view.checkedAt} />}
    >
      {exposed.length === 0 ? (
        <p className="px-4 py-5 text-sm text-muted">
          {view.checkedAt ? (
            <>Nothing exposed in the last successful check, {timeAgo(view.checkedAt, now)}.</>
          ) : (
            <>No successful check yet, so exposure is unknown.</>
          )}
        </p>
      ) : (
        <ul className="divide-y divide-line">
          {exposed.map(({ finding, resource, session }) => (
            <li key={finding.id}>
              <button
                type="button"
                onClick={() => open(finding.resource_id, "exposure")}
                className="relative flex w-full flex-col gap-1 py-2.5 pr-4 pl-5 text-left hover:bg-danger-wash"
              >
                <span className="absolute inset-y-2 left-0 w-[3px] rounded-r bg-danger" aria-hidden="true" />
                <span className="flex items-center gap-2">
                  <span className="font-mono text-2xs font-medium tracking-wider text-danger uppercase">{finding.severity}</span>
                  <span className="truncate text-[13px] font-medium text-danger">{finding.title}</span>
                </span>
                <span className="flex min-w-0 items-center gap-2 text-xs">
                  <span className="font-mono text-faint">{resource ? kindLabel(resource.kind) : ""}</span>
                  <span className="truncate font-mono text-fg">{resource?.name ?? finding.resource_id}</span>
                  <span className="text-faint">·</span>
                  {session ? (
                    <span className="min-w-0 truncate text-muted">
                      {session.developer_name} · <span className="font-mono">{shortId(session.session.id)}</span>
                    </span>
                  ) : (
                    <span className="text-muted">creator unknown</span>
                  )}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {view.unknownFindings.length > 0 ? (
        <p className="border-t border-line px-4 py-2 text-xs text-muted">
          <Icon name="question" className="mr-1 inline size-3.5 -translate-y-px text-faint" />
          {plural(view.unknownFindings.length, "check")} couldn&apos;t run, so exposure is unknown for{" "}
          {view.unknownFindings.map((u, i) => (
            <span key={u.finding.id}>
              {i > 0 ? ", " : ""}
              <button
                type="button"
                className="font-mono text-fg underline decoration-line-strong underline-offset-2 hover:decoration-fg"
                onClick={() => open(u.finding.resource_id, "exposure")}
              >
                {u.resource?.name ?? u.finding.resource_id}
              </button>
            </span>
          ))}
          .
        </p>
      ) : null}
      <p className="flex items-center gap-2 border-t border-line px-4 py-2 text-xs text-faint">
        <Icon name="lock" className="size-3.5" />
        Muretto never runs fixes. It shows the exact fix; you run it.
      </p>
    </Panel>
  );
}

// ------------------------------------------------------------------ review queue

export function ReviewQueue() {
  const { view, open } = useDash();
  const { now } = useClock();
  const items = [...view.answer.reviews].sort((a, b) => {
    const pa = a.state === "pending" ? 0 : 1;
    const pb = b.state === "pending" ? 0 : 1;
    return pa - pb || Date.parse(a.due_at) - Date.parse(b.due_at);
  });

  return (
    <Panel
      id="review"
      title={
        <>
          Still need this?
          <span className="font-normal text-faint">review queue</span>
        </>
      }
      aside={<span className="rounded border border-line px-1.5 py-0.5 text-2xs uppercase tracking-wider">read-only</span>}
    >
      {items.length === 0 ? (
        <p className="px-4 py-5 text-sm text-muted">Nothing to review.</p>
      ) : (
        <ul className="scroll-thin max-h-[280px] divide-y divide-line overflow-y-auto">
          {items.map((item) => {
            const r = view.resource.get(item.resource_id);
            const days = daysUntil(item.due_at, now);
            const overdue = item.state === "pending" && days < 0;
            const owner = item.owner_developer_id ? view.developerName.get(item.owner_developer_id) : undefined;
            const decider = item.decided_by ? view.developerName.get(item.decided_by) ?? "someone" : undefined;
            return (
              <li key={item.resource_id}>
                <button
                  type="button"
                  onClick={() => open(item.resource_id)}
                  className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 px-4 py-2 text-left hover:bg-hover"
                >
                  <span className="flex min-w-0 items-center gap-2 font-mono text-[12.5px]">
                    <span className="text-faint">{r ? kindLabel(r.kind) : ""}</span>
                    <span className={`truncate ${item.state !== "pending" ? "text-muted" : ""}`}>{r?.name ?? item.resource_id}</span>
                  </span>
                  <span className="tabular row-span-2 text-right text-xs">
                    {item.state === "pending" ? (
                      overdue ? (
                        <span className="inline-flex items-center gap-1.5 font-medium text-fg">
                          <span className="size-1.5 rounded-full bg-accent" aria-hidden="true" />
                          overdue {timeAgo(item.due_at, now).replace(" ago", "")}
                        </span>
                      ) : (
                        <span className="text-muted">due {timeAgo(item.due_at, now)}</span>
                      )
                    ) : (
                      <span className="text-muted">
                        {item.state === "keep" ? "keep" : "not needed"}
                        {decider ? ` · ${decider}` : ""}
                      </span>
                    )}
                  </span>
                  <span className="text-xs text-muted">
                    {owner ? (
                      <>owner {owner}</>
                    ) : (
                      <span className="border-b border-dashed border-line-strong">no owner: creator unknown</span>
                    )}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
      <p className="border-t border-line px-4 py-2 text-xs text-faint">
        Decide with <code className="font-mono text-muted">muretto review</code>. It records the decision; nothing is deleted.
      </p>
    </Panel>
  );
}

// ------------------------------------------------------------------ cost

export function CostPanel({ scope }: { scope: "me" | "team" }) {
  const { view } = useDash();
  const rows: { key: string; label: React.ReactNode; ids: string[] }[] = [];

  if (scope === "team") {
    const byDev = new Map<string, { name: string; ids: string[] }>();
    for (const s of view.sessions) {
      const e = byDev.get(s.session.developer_id) ?? { name: s.developer_name, ids: [] };
      e.ids.push(...s.resource_ids);
      byDev.set(s.session.developer_id, e);
    }
    for (const [id, e] of byDev) rows.push({ key: id, label: e.name, ids: e.ids });
  } else {
    for (const s of view.sessions) {
      rows.push({
        key: s.session.id,
        label: (
          <span className="flex min-w-0 items-center gap-2">
            <span className="font-mono text-accent">{shortId(s.session.id)}</span>
            <span className="truncate text-muted">{s.session.task_title}</span>
          </span>
        ),
        ids: s.resource_ids,
      });
    }
  }
  if (view.unknownCreator.length > 0) {
    rows.push({
      key: "unknown",
      label: <span className="text-muted">creator unknown</span>,
      ids: view.unknownCreator.map((r) => r.id),
    });
  }

  return (
    <Panel
      id="cost"
      title={
        <>
          Cost
          <span className="font-normal text-faint">{scope === "team" ? "per developer" : "per session"}</span>
        </>
      }
      aside={<span>reported and estimated kept apart</span>}
    >
      <div className="overflow-x-auto">
        <table className="w-full min-w-[520px] text-left text-[13px]">
          <thead>
            <tr className="border-b border-line text-xs text-muted">
              <th scope="col" className="px-4 py-2 font-medium">
                {scope === "team" ? "Developer" : "Session"}
              </th>
              <th scope="col" className="px-4 py-2 font-medium" title="From the provider's billing data">
                Reported
              </th>
              <th scope="col" className="px-4 py-2 font-medium" title="Computed by Muretto from list prices">
                Estimated
              </th>
              <th scope="col" className="px-4 py-2 font-medium" title="No cost data. Never counted as $0.">
                Unknown
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {rows.map((row) => {
              const c = summarizeCosts(view, row.ids);
              return (
                <tr key={row.key} className="align-top">
                  <th scope="row" className="max-w-[16rem] px-4 py-2 font-medium">
                    {row.label}
                  </th>
                  <td className="px-4 py-2">
                    <CostAmounts groups={c.groups} source="reported" />
                  </td>
                  <td className="px-4 py-2">
                    <CostAmounts groups={c.groups} source="estimated" />
                  </td>
                  <td className="px-4 py-2">
                    <UnknownCost count={c.unknown} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <p className="border-t border-line px-4 py-2 text-xs text-faint">
        Reported comes from the provider. Estimated (~) is computed from list prices. Unknown is never shown as $0.
      </p>
    </Panel>
  );
}
