"use client";

import { createContext, useContext } from "react";
import type { ExposureFinding, Resource } from "@/contract";
import { daysUntil, kindLabel, money, timeAgo } from "@/lib/format";
import { headlineFinding, type CostGroup, type CostSummary, type View } from "@/lib/model";
import { useClock } from "../clock";
import { AttributionLabel, Icon } from "../ui";

// ------------------------------------------------------------------ context

export interface DashContext {
  view: View;
  sample: boolean;
  open: (resourceId: string, focus?: "exposure") => void;
}

export const Dash = createContext<DashContext | null>(null);

export function useDash(): DashContext {
  const ctx = useContext(Dash);
  if (!ctx) throw new Error("useDash outside <Dash.Provider>");
  return ctx;
}

// ------------------------------------------------------------------ exposure

export function ExposureText({ finding, compact = false }: { finding?: ExposureFinding; compact?: boolean }) {
  if (!finding) return null;
  if (finding.state === "exposed") {
    return (
      <span className="inline-flex min-w-0 items-center gap-1.5 text-danger">
        <Icon name="alert" className="size-3.5 shrink-0" />
        <span className="truncate">{finding.title}</span>
      </span>
    );
  }
  if (finding.state === "unknown") {
    return (
      <span className="inline-flex items-center gap-1.5 text-muted" title={finding.explanation}>
        <Icon name="question" className="size-3.5 shrink-0" />
        {compact ? "unknown" : "exposure unknown"}
      </span>
    );
  }
  if (compact) return null;
  return <span className="text-faint">{finding.title}</span>;
}

// ------------------------------------------------------------------ resource line

function ExistenceMark({ r }: { r: Resource }) {
  if (r.existence === "gone")
    return (
      <span className="text-faint" title="Gone: missing from a successful check after being seen">
        <Icon name="x" className="size-3.5" />
      </span>
    );
  if (r.existence === "unconfirmed")
    return (
      <span className="text-faint" title="Unconfirmed: known only from session evidence">
        <Icon name="question" className="size-3.5" />
      </span>
    );
  return (
    <span className="text-accent" title="Confirmed by the last successful check">
      <Icon name="check" className="size-3.5" />
    </span>
  );
}

/** One resource in the git-log view. The whole row opens the resource detail. */
export function ResourceLine({ r }: { r: Resource }) {
  const { view, open } = useDash();
  const { now } = useClock();
  const a = view.attribution.get(r.id);
  const f = headlineFinding(view, r.id);
  const review = view.review.get(r.id);
  const due = review && review.state === "pending" && daysUntil(review.due_at, now) < 0;
  const created = r.created_at ?? a?.evidence[0]?.occurred_at;
  const ageDays = created ? -daysUntil(created, now) - 1 : 0;

  return (
    <li>
      <button
        type="button"
        onClick={() => open(r.id, f?.state === "exposed" ? "exposure" : undefined)}
        className="group grid w-full grid-cols-[16px_76px_minmax(0,1fr)_auto] items-center gap-x-3 rounded-md px-2 py-[5px] text-left font-mono text-[12.5px] hover:bg-hover sm:grid-cols-[16px_76px_minmax(0,13rem)_auto_minmax(0,1fr)]"
      >
        <ExistenceMark r={r} />
        <span className="text-faint">{kindLabel(r.kind)}</span>
        <span className={`truncate ${r.existence === "gone" ? "text-faint line-through" : "text-fg"}`}>{r.name}</span>
        <AttributionLabel label={a?.label} />
        <span className="col-span-4 flex min-w-0 items-center gap-3 pl-[104px] font-sans text-xs sm:col-span-1 sm:pl-0">
          <ExposureText finding={f} />
          {r.existence === "unconfirmed" ? <span className="text-faint">unconfirmed</span> : null}
          {r.existence === "gone" ? <span className="text-faint">gone {timeAgo(r.gone_at, now)}</span> : null}
          {due ? (
            <span className="whitespace-nowrap text-fg">
              {ageDays} days old · <span className="text-accent">review due</span>
            </span>
          ) : null}
        </span>
      </button>
    </li>
  );
}

// ------------------------------------------------------------------ cost

function intervalSuffix(g: CostGroup): string {
  if (g.interval === "monthly") return "/mo";
  if (g.interval === "hourly") return "/h";
  if (g.interval === "period") return " this period";
  return "";
}

export function CostAmounts({ groups, source }: { groups: CostGroup[]; source: "reported" | "estimated" }) {
  const list = groups.filter((g) => g.source === source);
  if (list.length === 0) return <span className="text-faint">none</span>;
  return (
    <span className="tabular flex flex-col">
      {list.map((g) => (
        <span key={`${g.interval}-${g.currency}`} className="whitespace-nowrap">
          {source === "estimated" ? "~" : ""}
          {money(g.micros, g.currency)}
          <span className="text-faint">{intervalSuffix(g)}</span>
        </span>
      ))}
    </span>
  );
}

export function UnknownCost({ count }: { count: number }) {
  if (count === 0) return <span className="text-faint">none</span>;
  return (
    <span
      className="whitespace-nowrap border-b border-dashed border-line-strong text-muted"
      title="No cost data for these resources. Unknown is never counted as $0."
    >
      {count} unknown
    </span>
  );
}

/** Compact one-line cost: "$3.48 reported · ~$10.00/mo est. · 4 unknown". */
export function CostInline({ cost }: { cost: CostSummary }) {
  const parts: React.ReactNode[] = [];
  for (const g of cost.groups) {
    parts.push(
      <span key={`${g.source}-${g.interval}-${g.currency}`} className="whitespace-nowrap">
        {g.source === "estimated" ? "~" : ""}
        {money(g.micros, g.currency)}
        {intervalSuffix(g)} <span className="text-faint">{g.source === "estimated" ? "est." : "reported"}</span>
      </span>,
    );
  }
  if (cost.unknown > 0) {
    parts.push(
      <span key="unknown" className="whitespace-nowrap text-faint" title="Unknown is never counted as $0.">
        cost unknown{cost.groups.length > 0 ? ` (${cost.unknown})` : ""}
      </span>,
    );
  }
  return (
    <span className="tabular inline-flex flex-wrap items-center gap-x-2">
      {parts.map((p, i) => (
        <span key={i} className="inline-flex items-center gap-2">
          {i > 0 ? <span className="text-line-strong">·</span> : null}
          {p}
        </span>
      ))}
    </span>
  );
}

/** The "checked 2 min ago" data-age label. Stale data (over an hour) is called out. */
export function DataAge({ at, className = "" }: { at?: string; className?: string }) {
  const { now } = useClock();
  if (!at) {
    return <span className={`text-muted ${className}`}>never checked</span>;
  }
  const stale = now - Date.parse(at) > 60 * 60 * 1000;
  return (
    <span
      className={`tabular whitespace-nowrap ${stale ? "text-fg" : "text-muted"} ${className}`}
      title={`Oldest successful provider check: ${at}`}
    >
      checked {timeAgo(at, now)}
      {stale ? <span className="ml-1 rounded bg-hover px-1 text-2xs font-medium uppercase tracking-wide">stale</span> : null}
    </span>
  );
}
