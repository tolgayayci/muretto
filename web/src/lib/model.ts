// A read-only index over one Answer, so components can join entities by id
// without each re-scanning the arrays. Pure: derive it with buildView(answer).
import type {
  Answer,
  Attribution,
  CostRecord,
  Evidence,
  ExposureFinding,
  Resource,
  ReviewItem,
  SessionView,
} from "@/contract";
import { ms } from "./format";

export type Severity = ExposureFinding["severity"];

const SEVERITY_ORDER: Record<Severity, number> = { critical: 0, high: 1, medium: 2, low: 3, info: 4 };

export interface FeedItem {
  key: string;
  at: string;
  resource: Resource;
  attribution?: Attribution;
  session?: SessionView;
}

export interface ExposedItem {
  finding: ExposureFinding;
  resource?: Resource;
  session?: SessionView;
}

export interface CostGroup {
  source: "reported" | "estimated";
  interval?: CostRecord["interval"];
  currency: string;
  micros: number;
}

export interface CostSummary {
  groups: CostGroup[];
  /** Resources with no reported or estimated amount. Never counted as $0. */
  unknown: number;
}

export interface View {
  answer: Answer;
  resource: Map<string, Resource>;
  attribution: Map<string, Attribution>;
  findings: Map<string, ExposureFinding[]>;
  costs: Map<string, CostRecord[]>;
  review: Map<string, ReviewItem>;
  session: Map<string, SessionView>;
  sessionOf: Map<string, SessionView>;
  developerName: Map<string, string>;
  /** Sessions newest first. */
  sessions: SessionView[];
  unknownCreator: Resource[];
  exposed: ExposedItem[];
  unknownFindings: ExposedItem[];
  feed: FeedItem[];
  /** The stalest successful provider check: what "checked … ago" refers to. */
  checkedAt?: string;
  failedChecks: number;
}

export function bySeverity(a: ExposureFinding, b: ExposureFinding): number {
  return SEVERITY_ORDER[a.severity] - SEVERITY_ORDER[b.severity];
}

function push<K, V>(m: Map<K, V[]>, k: K, v: V) {
  const list = m.get(k);
  if (list) list.push(v);
  else m.set(k, [v]);
}

/** The earliest creating action behind an exact attribution. */
export function firstEvidence(a: Attribution | undefined): Evidence | undefined {
  if (!a || a.evidence.length === 0) return undefined;
  return [...a.evidence].sort((x, y) => ms(x.occurred_at) - ms(y.occurred_at))[0];
}

export function buildView(answer: Answer): View {
  const resource = new Map(answer.resources.map((r) => [r.id, r]));
  const attribution = new Map(answer.attributions.map((a) => [a.resource_id, a]));
  const review = new Map(answer.reviews.map((r) => [r.resource_id, r]));
  const session = new Map(answer.sessions.map((s) => [s.session.id, s]));
  const findings = new Map<string, ExposureFinding[]>();
  const costs = new Map<string, CostRecord[]>();
  for (const f of answer.findings) push(findings, f.resource_id, f);
  for (const c of answer.costs) push(costs, c.resource_id, c);
  for (const list of findings.values()) list.sort(bySeverity);

  const developerName = new Map<string, string>();
  developerName.set(answer.developer.id, answer.developer.display_name);
  const sessionOf = new Map<string, SessionView>();
  for (const s of answer.sessions) {
    developerName.set(s.session.developer_id, s.developer_name);
    for (const id of s.resource_ids) sessionOf.set(id, s);
  }
  // An exact attribution names its session even if the session view omitted the id.
  for (const a of answer.attributions) {
    if (a.label === "exact" && a.session_id && !sessionOf.has(a.resource_id)) {
      const s = session.get(a.session_id);
      if (s) sessionOf.set(a.resource_id, s);
    }
  }

  const sessions = [...answer.sessions].sort((a, b) => ms(b.session.started_at) - ms(a.session.started_at));

  const unknownCreator = answer.resources
    .filter((r) => attribution.get(r.id)?.label !== "exact")
    .sort((a, b) => a.kind.localeCompare(b.kind) || a.name.localeCompare(b.name));

  const toItem = (f: ExposureFinding): ExposedItem => ({
    finding: f,
    resource: resource.get(f.resource_id),
    session: sessionOf.get(f.resource_id),
  });
  const exposed = answer.findings.filter((f) => f.state === "exposed").sort(bySeverity).map(toItem);
  const unknownFindings = answer.findings.filter((f) => f.state === "unknown").sort(bySeverity).map(toItem);

  const feed: FeedItem[] = answer.resources
    .map((r) => {
      const a = attribution.get(r.id);
      const exact = a?.label === "exact";
      const at = (exact ? firstEvidence(a)?.occurred_at : undefined) ?? r.created_at ?? r.first_seen_at;
      return { key: r.id, at, resource: r, attribution: a, session: exact ? sessionOf.get(r.id) : undefined };
    })
    .sort((a, b) => ms(b.at) - ms(a.at));

  const okChecks = answer.checks.filter((c) => c.last_success_at);
  const checkedAt =
    answer.summary.oldest_check_at ??
    okChecks.map((c) => c.last_success_at!).sort((a, b) => ms(a) - ms(b))[0];

  return {
    answer,
    resource,
    attribution,
    findings,
    costs,
    review,
    session,
    sessionOf,
    developerName,
    sessions,
    unknownCreator,
    exposed,
    unknownFindings,
    feed,
    checkedAt,
    failedChecks: answer.checks.filter((c) => c.status === "error").length,
  };
}

/** Sums cost records, keeping reported and estimated (and each interval) apart. */
export function summarizeCosts(view: View, resourceIds: Iterable<string>): CostSummary {
  const groups = new Map<string, CostGroup>();
  let unknown = 0;
  for (const id of resourceIds) {
    const records = (view.costs.get(id) ?? []).filter(
      (c) => c.source !== "unknown" && typeof c.amount_micros === "number",
    );
    if (records.length === 0) {
      unknown += 1;
      continue;
    }
    for (const c of records) {
      const source = c.source as "reported" | "estimated";
      const currency = c.currency ?? "USD";
      const key = `${source}|${c.interval ?? ""}|${currency}`;
      const g = groups.get(key) ?? { source, interval: c.interval, currency, micros: 0 };
      g.micros += c.amount_micros!;
      groups.set(key, g);
    }
  }
  const order = { reported: 0, estimated: 1 };
  return {
    groups: [...groups.values()].sort((a, b) => order[a.source] - order[b.source]),
    unknown,
  };
}

/** Resource ids grouped for the git-log view: provider, then parent. */
export interface ResourceGroup {
  provider: string;
  parent?: Resource;
  parentId?: string;
  resources: Resource[];
}

const KIND_ORDER = [
  "project",
  "branch",
  "table",
  "bucket",
  "function",
  "deployment",
  "domain",
  "env_var",
];

function kindRank(kind: string): number {
  const i = KIND_ORDER.indexOf(kind.slice(kind.indexOf(".") + 1));
  return i === -1 ? KIND_ORDER.length : i;
}

export function groupResources(view: View, ids: string[]): ResourceGroup[] {
  const groups = new Map<string, ResourceGroup>();
  for (const id of ids) {
    const r = view.resource.get(id);
    if (!r) continue;
    // A session that created the container itself lists it at the top of its own group.
    const key = `${r.provider}|${r.parent_id ?? ""}`;
    const g = groups.get(key) ?? {
      provider: r.provider,
      parentId: r.parent_id,
      parent: r.parent_id ? view.resource.get(r.parent_id) : undefined,
      resources: [],
    };
    g.resources.push(r);
    groups.set(key, g);
  }
  for (const g of groups.values()) {
    g.resources.sort((a, b) => kindRank(a.kind) - kindRank(b.kind) || a.name.localeCompare(b.name));
  }
  return [...groups.values()].sort((a, b) => a.provider.localeCompare(b.provider));
}

/** Name of the resource with the most severe exposure among ids (for "muretto fix <name>" hints). */
export function worstExposedName(view: View, ids: string[]): string | undefined {
  const set = new Set(ids);
  const item = view.exposed.find((e) => set.has(e.finding.resource_id));
  return item?.resource?.name;
}

/** Most severe finding for a resource, preferring exposed over unknown over not_exposed. */
export function headlineFinding(view: View, resourceId: string): ExposureFinding | undefined {
  const list = view.findings.get(resourceId) ?? [];
  return (
    list.find((f) => f.state === "exposed") ??
    list.find((f) => f.state === "unknown") ??
    list.find((f) => f.state === "not_exposed")
  );
}
