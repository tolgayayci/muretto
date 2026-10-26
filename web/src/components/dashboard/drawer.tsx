"use client";

import { useEffect, useRef, type ReactNode } from "react";
import type { ExposureFinding, Resource } from "@/contract";
import { daysUntil, kindLabel, providerLabel, sessionSpan, shortId, stamp, timeAgo } from "@/lib/format";
import { summarizeCosts } from "@/lib/model";
import { useClock } from "../clock";
import { AttributionLabel, CopyButton, Icon } from "../ui";
import { CostAmounts, UnknownCost, useDash } from "./parts";

function Step({
  title,
  tone = "default",
  children,
  id,
  last = false,
}: {
  title: string;
  tone?: "default" | "danger" | "muted";
  children: ReactNode;
  id?: string;
  last?: boolean;
}) {
  const dot =
    tone === "danger" ? "border-danger bg-danger" : tone === "muted" ? "border-line-strong bg-panel" : "border-accent bg-panel";
  return (
    <li id={id} className="relative scroll-mt-24 pb-6 pl-7">
      {!last ? <span className="absolute top-4 bottom-0 left-[5px] w-px bg-line" aria-hidden="true" /> : null}
      <span className={`absolute top-[5px] left-0 size-[11px] rounded-full border-2 ${dot}`} aria-hidden="true" />
      <h3 className={`mb-2 text-xs font-medium tracking-[0.06em] uppercase ${tone === "danger" ? "text-danger" : "text-muted"}`}>
        {title}
      </h3>
      <div className="text-[13px] leading-relaxed">{children}</div>
    </li>
  );
}

function Code({ children, copy }: { children: string; copy?: boolean }) {
  return (
    <div className="group relative mt-1 rounded-lg border border-line bg-sunken">
      <pre className="scroll-thin overflow-x-auto px-3 py-2.5 font-mono text-[12px] leading-[1.6] whitespace-pre text-fg">{children}</pre>
      {copy ? (
        <div className="absolute top-1.5 right-1.5">
          <CopyButton text={children} variant="solid" />
        </div>
      ) : null}
    </div>
  );
}

function Meta({ items }: { items: [string, ReactNode][] }) {
  return (
    <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1 text-xs">
      {items.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-faint">{k}</dt>
          <dd className="min-w-0 truncate text-muted">{v}</dd>
        </div>
      ))}
    </dl>
  );
}

function FindingBlock({ f }: { f: ExposureFinding }) {
  const { now, timeZone } = useClock();
  const checked = f.checked_at ? (
    <span title={stamp(f.checked_at, timeZone)}>checked {timeAgo(f.checked_at, now)}</span>
  ) : (
    "never checked"
  );
  if (f.state === "not_exposed") {
    return (
      <div className="flex flex-wrap items-center gap-x-2 text-xs text-muted">
        <Icon name="check" className="size-3.5 text-accent" />
        <span className="text-fg">{f.title}</span>
        <span className="font-mono text-faint">{f.rule_id}</span>
        <span className="text-faint">· {checked}</span>
      </div>
    );
  }
  const exposed = f.state === "exposed";
  return (
    <div className={`rounded-lg border p-3 ${exposed ? "border-danger-line bg-danger-wash" : "border-dashed border-line-strong"}`}>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className={`inline-flex items-center gap-1.5 font-medium ${exposed ? "text-danger" : "text-fg"}`}>
          <Icon name={exposed ? "alert" : "question"} className="size-4" />
          {f.title}
        </span>
        <span className={`font-mono text-2xs uppercase ${exposed ? "text-danger" : "text-muted"}`}>
          {exposed ? f.severity : "unknown"}
        </span>
      </div>
      <p className="mt-1.5 text-muted">{f.explanation}</p>
      {f.caveat ? <p className="mt-1 text-xs text-faint">Can&apos;t see: {f.caveat}</p> : null}
      <p className="mt-1.5 text-xs text-faint">
        <span className="font-mono">{f.rule_id}</span> · signal {f.signal.replace(/_/g, " ")} · {checked}
      </p>
      {exposed ? (
        <div className="mt-3">
          <div className="flex items-center justify-between text-xs">
            <span className="font-medium text-fg">
              Fix <span className="font-normal text-faint">({f.fix.kind}) · run it yourself</span>
            </span>
          </div>
          <Code copy>{f.fix.text}</Code>
          {f.fix.note ? <p className="mt-1.5 text-xs text-muted">{f.fix.note}</p> : null}
          {f.fix.docs_url ? (
            <a
              href={f.fix.docs_url}
              target="_blank"
              rel="noreferrer noopener"
              className="mt-1.5 inline-flex items-center gap-1 text-xs text-muted underline decoration-line-strong underline-offset-2 hover:text-fg"
            >
              Provider docs <Icon name="external" className="size-3" />
            </a>
          ) : null}
          <p className="mt-2 flex items-center gap-1.5 text-xs text-faint">
            <Icon name="lock" className="size-3.5" />
            Muretto never runs this. Copy it and run it yourself.
          </p>
        </div>
      ) : (
        <p className="mt-2 text-xs text-muted">{f.fix.text}</p>
      )}
    </div>
  );
}

function Detail({ r }: { r: Resource }) {
  const { view, sample } = useDash();
  const { now, timeZone } = useClock();
  const a = view.attribution.get(r.id);
  const s = a?.label === "exact" ? view.sessionOf.get(r.id) : undefined;
  const findings = view.findings.get(r.id) ?? [];
  const review = view.review.get(r.id);
  const cost = summarizeCosts(view, [r.id]);
  const costRecords = view.costs.get(r.id) ?? [];
  const parent = r.parent_id ? view.resource.get(r.parent_id) : undefined;
  const anyExposed = findings.some((f) => f.state === "exposed");
  const owner = review?.owner_developer_id ? view.developerName.get(review.owner_developer_id) : undefined;
  const attrs = Object.entries(r.attributes).filter(([, v]) => v !== null && v !== "");

  return (
    <ol className="px-6 pt-6">
      <Step title="Prompt" tone={s ? "default" : "muted"}>
        {s ? (
          <>
            <p className="text-[15px] text-fg">“{s.session.task_title ?? "no task title"}”</p>
            <p className="mt-1 text-xs text-muted">
              <span className="font-medium text-fg">{s.developer_name}</span> · session{" "}
              <span className="font-mono text-accent">{shortId(s.session.id)}</span> · Claude Code
              {s.session.agent_version ? ` ${s.session.agent_version}` : ""} ·{" "}
              {sessionSpan(s.session.started_at, s.session.ended_at, now, timeZone)}
            </p>
            {s.session.repo?.display_name ? (
              <p className="mt-0.5 font-mono text-xs text-faint">
                {s.session.repo.display_name}
                {s.session.git_branch ? ` (${s.session.git_branch})` : ""}
              </p>
            ) : null}
          </>
        ) : (
          <p className="text-muted">
            <span className="font-medium text-fg">Creator unknown.</span> {a?.reason ?? "No recorded agent session created this."}{" "}
            Muretto never guesses.
          </p>
        )}
      </Step>

      <Step title="Command" tone={a?.evidence.length ? "default" : "muted"}>
        {a && a.evidence.length > 0 ? (
          <div className="flex flex-col gap-3">
            {a.evidence.map((e) => (
              <div key={e.action_id}>
                <p className="flex flex-wrap items-center gap-x-2 text-xs">
                  <span className="font-mono text-fg">{e.tool_name}</span>
                  <span className="text-faint" title={stamp(e.occurred_at, timeZone)}>
                    {timeAgo(e.occurred_at, now)}
                  </span>
                  <span className="text-faint">· redacted excerpt</span>
                </p>
                <Code>{e.excerpt}</Code>
                <p className="mt-1 text-xs text-faint">
                  matched <span className="font-mono text-muted">{e.matched_value}</span> via{" "}
                  <span className="font-mono">{e.signature_id}</span>
                </p>
              </div>
            ))}
            <p className="text-xs text-muted">
              <AttributionLabel label="exact" className="mr-1.5" />
              {a.reason}
              {a.rule_id ? <span className="font-mono text-faint"> ({a.rule_id})</span> : null}
            </p>
          </div>
        ) : (
          <p className="text-muted">No creating command recorded.</p>
        )}
      </Step>

      <Step title="Resource">
        <Meta
          items={[
            ["provider", `${providerLabel(r.provider)}${r.account_name ? ` · ${r.account_name}` : ""}`],
            ["id", <span key="id" className="font-mono">{r.provider_id}</span>],
            ...(parent ? ([["in", `${kindLabel(parent.kind)} ${parent.name}`]] as [string, ReactNode][]) : []),
            [
              "created",
              r.created_at ? (
                <span key="c" title={stamp(r.created_at, timeZone)}>
                  {timeAgo(r.created_at, now)}
                </span>
              ) : (
                "not reported by the provider"
              ),
            ],
            ...(r.url
              ? ([
                  [
                    "url",
                    sample ? (
                      <span key="u" className="font-mono">{r.url}</span>
                    ) : (
                      <a key="u" href={r.url} target="_blank" rel="noreferrer noopener" className="font-mono underline decoration-line-strong underline-offset-2 hover:text-fg">
                        {r.url}
                      </a>
                    ),
                  ],
                ] as [string, ReactNode][])
              : []),
            ...attrs.map(([k, v]) => [k.replace(/_/g, " "), String(v)] as [string, ReactNode]),
          ]}
        />
      </Step>

      <Step title="Current state" tone={r.existence === "confirmed" ? "default" : "muted"}>
        <p className="text-fg">
          {r.existence === "confirmed" ? (
            <>
              Exists{r.status ? <span className="text-muted"> · {r.status}</span> : null}
              <span className="text-muted"> · seen {timeAgo(r.last_seen_at, now)}</span>
            </>
          ) : r.existence === "gone" ? (
            <>
              Gone <span className="text-muted">since {timeAgo(r.gone_at ?? r.last_seen_at, now)}, missing from a successful check</span>
            </>
          ) : (
            <>
              Unconfirmed{" "}
              <span className="text-muted">
                · known only from session evidence ({r.seen_via}); no successful list call covers it
              </span>
            </>
          )}
        </p>
        <div className="mt-2 grid grid-cols-3 gap-2 rounded-lg border border-line p-2.5 text-xs">
          <div>
            <p className="text-faint">Reported</p>
            <CostAmounts groups={cost.groups} source="reported" />
          </div>
          <div>
            <p className="text-faint">Estimated</p>
            <CostAmounts groups={cost.groups} source="estimated" />
          </div>
          <div>
            <p className="text-faint">Unknown</p>
            <UnknownCost count={cost.unknown} />
          </div>
          {costRecords[0]?.basis ? <p className="col-span-3 text-faint">{costRecords[0].basis}</p> : null}
        </div>
      </Step>

      <Step title="Exposure" tone={anyExposed ? "danger" : findings.length ? "default" : "muted"} id="drawer-exposure">
        {findings.length > 0 ? (
          <div className="flex flex-col gap-2">
            {findings.map((f) => (
              <FindingBlock key={f.id} f={f} />
            ))}
          </div>
        ) : (
          <p className="text-muted">No exposure rule covers this kind yet.</p>
        )}
      </Step>

      <Step title="Review" tone={review ? "default" : "muted"} last>
        {review ? (
          <p className="text-fg">
            {review.state === "pending" ? (
              <>
                Still need this?{" "}
                <span className="text-muted">
                  {daysUntil(review.due_at, now) < 0
                    ? `overdue ${timeAgo(review.due_at, now).replace(" ago", "")}`
                    : `due ${timeAgo(review.due_at, now)}`}
                  {" · "}
                  {owner ? `owner ${owner}` : "no owner (creator unknown)"}
                </span>
              </>
            ) : (
              <>
                Decided: {review.state === "keep" ? "keep" : "not needed"}
                <span className="text-muted">
                  {review.decided_by ? ` by ${view.developerName.get(review.decided_by) ?? "someone"}` : ""}
                  {review.decided_at ? `, ${timeAgo(review.decided_at, now)}` : ""}
                  {review.note ? `: “${review.note}”` : ""}
                </span>
              </>
            )}
          </p>
        ) : (
          <p className="text-muted">No review item.</p>
        )}
      </Step>
    </ol>
  );
}

export function ResourceDrawer({
  resourceId,
  focus,
  onClose,
}: {
  resourceId: string | null;
  focus?: "exposure";
  onClose: () => void;
}) {
  const { view } = useDash();
  const ref = useRef<HTMLDialogElement>(null);
  const returnFocus = useRef<HTMLElement | null>(null);
  const r = resourceId ? view.resource.get(resourceId) : undefined;
  const a = r ? view.attribution.get(r.id) : undefined;

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (r && !d.open) {
      returnFocus.current = document.activeElement as HTMLElement | null;
      d.showModal();
      d.querySelector<HTMLElement>("[data-autofocus]")?.focus();
      if (focus === "exposure") {
        requestAnimationFrame(() => d.querySelector("#drawer-exposure")?.scrollIntoView({ block: "start" }));
      } else {
        d.querySelector(".drawer-body")?.scrollTo({ top: 0 });
      }
    } else if (!r && d.open) {
      d.close();
    }
  }, [r, focus]);

  return (
    <dialog
      ref={ref}
      className="drawer"
      aria-labelledby="drawer-title"
      onClose={() => {
        onClose();
        returnFocus.current?.focus();
      }}
      onClick={(e) => {
        if (e.target === ref.current) ref.current?.close();
      }}
    >
      {r ? (
        <div className="flex h-full flex-col">
          <header className="flex items-start justify-between gap-4 border-b border-line px-6 py-4">
            <div className="min-w-0">
              <p className="flex items-center gap-2 text-xs text-muted">
                <span>{providerLabel(r.provider)}</span>
                <span className="text-faint">/</span>
                <span>{kindLabel(r.kind)}</span>
              </p>
              <h2 id="drawer-title" className="mt-1 truncate font-mono text-lg font-medium tracking-[-0.01em]">
                {r.name}
              </h2>
              <div className="mt-2 flex items-center gap-2">
                <AttributionLabel label={a?.label} />
                <code className="truncate font-mono text-2xs text-faint">{r.id}</code>
              </div>
            </div>
            <button
              type="button"
              data-autofocus
              onClick={() => ref.current?.close()}
              className="inline-flex size-8 shrink-0 items-center justify-center rounded-md text-muted hover:bg-hover hover:text-fg"
              aria-label="Close resource detail"
            >
              <Icon name="x" />
            </button>
          </header>
          <div className="drawer-body scroll-thin min-h-0 flex-1 overflow-y-auto">
            <Detail r={r} />
            <div className="mx-6 mb-6 rounded-lg border border-line p-3">
              <p className="mb-2 text-xs text-faint">Same view in the terminal</p>
              <div className="flex flex-col gap-1 font-mono text-[12px]">
                {[`muretto show ${r.name}`, `muretto why ${r.name}`, `muretto fix ${r.name}`].map((c) => (
                  <div key={c} className="flex items-center justify-between gap-2">
                    <span className="text-muted">
                      <span className="text-faint">$ </span>
                      {c}
                    </span>
                    <CopyButton text={c} />
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      ) : null}
    </dialog>
  );
}
