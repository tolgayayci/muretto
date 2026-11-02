// Derives everything the dashboard shows from one Answer (the same computed
// model the CLI renders). Pure functions: no I/O, the clock is passed in.
import type { Answer, Attribution, Evidence, ExposureFinding, ProviderCheck, Resource, ReviewItem, Session, Warning } from "@/contract";

const DAY = 86_400_000;

/** Whole calendar days (UTC) from a to b. */
function calendarDays(a: Date, b: Date): number {
  const day = (d: Date) => Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate());
  return Math.round((day(b) - day(a)) / DAY);
}

export type ResourceStatus = "open" | "cant_tell" | "waiting" | "unconfirmed" | "gone" | "ok";
export type OverviewState = "new" | "waiting" | "clear" | "attention";

export interface Finding {
  id: string;
  ruleId: string;
  severity: ExposureFinding["severity"];
  state: ExposureFinding["state"];
  title: string;
  explanation: string;
  caveat?: string;
  /** How Muretto knows: the provider's advisor, an API field, a catalog query or a name pattern. */
  signal: ExposureFinding["signal"];
  checkedAt?: Date;
  /** One plain sentence: what anyone on the internet can do. */
  what: string;
  /** One plain sentence: why. */
  why: string;
}

export interface Review {
  state: ReviewItem["state"];
  dueAt: Date;
  /** Whole days past due; 0 or less means not due yet. */
  overdueDays: number;
  decidedAt?: Date;
  note?: string;
}

export interface Res {
  id: string;
  name: string;
  /** Short plain kind: table, bucket, function, branch, project, preview, deployment, env var, domain. */
  kind: string;
  provider: string;
  account: string;
  project: string;
  projectId: string;
  url?: string;
  existence: Resource["existence"];
  goneAt?: Date;
  createdAt?: Date;
  firstSeenAt: Date;
  lastSeenAt: Date;
  /** Days since it was created (or first seen). */
  ageDays: number;
  origin: "agent" | "unknown";
  sessionId?: string;
  originReason: string;
  evidence: Evidence[];
  findings: Finding[];
  open: Finding[];
  cantTell: Finding[];
  review?: Review;
  status: ResourceStatus;
}

export interface Ses {
  id: string;
  /** Short id for display, e.g. "8f2a". */
  short: string;
  title: string;
  agent: string;
  agentVersion?: string;
  model?: string;
  repo?: string;
  branch?: string;
  startedAt: Date;
  endedAt?: Date;
  lastActivityAt?: Date;
  live: boolean;
  durationMin: number;
  transcriptOnly: boolean;
  resourceIds: string[];
  openCount: number;
  waitingCount: number;
}

export interface Project {
  id: string;
  name: string;
  provider: string;
  account: string;
  resources: Res[];
  open: number;
  waiting: number;
}

export interface CheckGroup {
  provider: string;
  account: string;
  accountId: string;
  checks: ProviderCheck[];
  ok: boolean;
  failing: ProviderCheck[];
  lastSuccessAt?: Date;
}

export interface Model {
  now: Date;
  developer: Answer["developer"];
  resources: Res[];
  sessions: Ses[];
  open: Res[];
  cantTell: Res[];
  waiting: Res[];
  comingUp: Res[];
  decided: Res[];
  unknownOrigin: Res[];
  gone: Res[];
  agentMade: Res[];
  projects: Project[];
  checks: CheckGroup[];
  warnings: Warning[];
  /** Newest successful check across providers. */
  checkedAt?: Date;
  state: OverviewState;
  hasProviders: boolean;
}

const SEVERITY_RANK: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3, info: 4 };

const PLAIN: Record<string, { what: string; why: string }> = {
  rls_disabled: {
    what: "Anyone can read every row.",
    why: "Row level security is off, so your app's public key is enough to read it all.",
  },
  public_bucket: {
    what: "Anyone can download the files.",
    why: "The bucket is public. Every upload has a link that works without signing in.",
  },
  public_preview: {
    what: "Anyone can open this preview.",
    why: "Deployment protection is off, so the preview link works for anyone who has it.",
  },
  client_exposed_env: {
    what: "A secret-looking key is sent to every browser.",
    why: "The variable is exposed to client code, so anyone can read it in the page source.",
  },
};

export function shortKind(r: Pick<Resource, "kind" | "attributes">): string {
  const k = r.kind.includes(".") ? r.kind.slice(r.kind.indexOf(".") + 1) : r.kind;
  switch (k) {
    case "deployment": {
      const target = r.attributes?.target;
      return target === "production" ? "deployment" : "preview";
    }
    case "env_var":
      return "env var";
    case "project_domain":
    case "domain":
      return "domain";
    case "edge_function":
    case "function":
      return "function";
    default:
      return k.replace(/_/g, " ");
  }
}

function date(v?: string | null): Date | undefined {
  if (!v) return undefined;
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? undefined : d;
}

function finding(f: ExposureFinding): Finding {
  const plain = PLAIN[f.rule_id] ?? { what: f.title, why: f.explanation };
  return {
    id: f.id,
    ruleId: f.rule_id,
    severity: f.severity,
    state: f.state,
    title: f.title,
    explanation: f.explanation,
    caveat: f.caveat,
    signal: f.signal,
    checkedAt: date(f.checked_at),
    what: plain.what,
    why: plain.why,
  };
}

export function shortId(id: string): string {
  return id.replace(/-/g, "").slice(0, 4);
}

function session(s: Session, resourceIds: string[], now: Date): Ses {
  const startedAt = date(s.started_at) ?? now;
  const endedAt = date(s.ended_at);
  const lastActivityAt = date(s.last_activity_at);
  const end = endedAt ?? lastActivityAt ?? now;
  const live = !endedAt && (lastActivityAt ? now.getTime() - lastActivityAt.getTime() < 15 * 60_000 : false);
  return {
    id: s.id,
    short: shortId(s.id),
    title: s.task_title?.trim() || "Untitled session",
    agent: s.agent,
    agentVersion: s.agent_version,
    model: s.model,
    repo: s.repo?.display_name,
    branch: s.git_branch,
    startedAt,
    endedAt,
    lastActivityAt,
    live,
    durationMin: Math.max(0, Math.round((end.getTime() - startedAt.getTime()) / 60_000)),
    transcriptOnly: !s.sources.includes("hook"),
    resourceIds,
    openCount: 0,
    waitingCount: 0,
  };
}

export function buildModel(a: Answer, now: Date = new Date()): Model {
  const raw = new Map(a.resources.map((r) => [r.id, r]));
  const attribution = new Map<string, Attribution>(a.attributions.map((x) => [x.resource_id, x]));
  const reviews = new Map<string, ReviewItem>(a.reviews.map((x) => [x.resource_id, x]));
  const findingsBy = new Map<string, ExposureFinding[]>();
  for (const f of a.findings) {
    const list = findingsBy.get(f.resource_id) ?? [];
    list.push(f);
    findingsBy.set(f.resource_id, list);
  }

  const projectOf = (r: Resource): Resource => {
    let cur = r;
    for (let i = 0; i < 6; i++) {
      if (cur.kind.endsWith(".project")) return cur;
      const p = cur.parent_id ? raw.get(cur.parent_id) : undefined;
      if (!p) break;
      cur = p;
    }
    return cur.kind.endsWith(".project") ? cur : r;
  };

  const resources: Res[] = a.resources.map((r) => {
    const att = attribution.get(r.id);
    const fs = (findingsBy.get(r.id) ?? []).map(finding).sort((x, y) => (SEVERITY_RANK[x.severity] ?? 9) - (SEVERITY_RANK[y.severity] ?? 9));
    const open = fs.filter((f) => f.state === "exposed");
    const cantTell = fs.filter((f) => f.state === "unknown");
    const rv = reviews.get(r.id);
    const createdAt = date(r.created_at);
    const firstSeenAt = date(r.first_seen_at) ?? now;
    const born = createdAt ?? firstSeenAt;
    const review: Review | undefined = rv
      ? {
          state: rv.state,
          dueAt: date(rv.due_at) ?? now,
          overdueDays: calendarDays(date(rv.due_at) ?? now, now),
          decidedAt: date(rv.decided_at),
          note: rv.note,
        }
      : undefined;
    const proj = projectOf(r);
    const gone = r.existence === "gone";
    const due = review?.state === "pending" && (date(rv?.due_at)?.getTime() ?? Infinity) <= now.getTime();
    const status: ResourceStatus = gone
      ? "gone"
      : open.length
        ? "open"
        : cantTell.length
          ? "cant_tell"
          : due
            ? "waiting"
            : r.existence === "unconfirmed"
              ? "unconfirmed"
              : "ok";
    return {
      id: r.id,
      name: r.name,
      kind: shortKind(r),
      provider: r.provider,
      account: r.account_name ?? r.account_id,
      project: proj.name,
      projectId: proj.id,
      url: r.url,
      existence: r.existence,
      goneAt: date(r.gone_at),
      createdAt,
      firstSeenAt,
      lastSeenAt: date(r.last_seen_at) ?? now,
      ageDays: Math.max(0, calendarDays(born, now)),
      origin: att?.label === "exact" && att.session_id ? "agent" : "unknown",
      sessionId: att?.label === "exact" ? att.session_id : undefined,
      originReason: att?.reason ?? "no recorded agent session created it",
      evidence: att?.evidence ?? [],
      findings: fs,
      open,
      cantTell,
      review,
      status,
    };
  });
  const byId = new Map(resources.map((r) => [r.id, r]));

  const sessions = a.sessions
    .map((sv) => session(sv.session, sv.resource_ids, now))
    .sort((x, y) => y.startedAt.getTime() - x.startedAt.getTime());
  for (const s of sessions) {
    const made = s.resourceIds.map((id) => byId.get(id)).filter((r): r is Res => !!r);
    s.openCount = made.filter((r) => r.status === "open").length;
    s.waitingCount = made.filter((r) => r.status === "waiting").length;
  }

  const live = resources.filter((r) => r.existence !== "gone");
  const bySeverity = (x: Res, y: Res) =>
    (SEVERITY_RANK[x.open[0]?.severity ?? x.cantTell[0]?.severity ?? "info"] ?? 9) - (SEVERITY_RANK[y.open[0]?.severity ?? y.cantTell[0]?.severity ?? "info"] ?? 9) ||
    (x.createdAt ?? x.firstSeenAt).getTime() - (y.createdAt ?? y.firstSeenAt).getTime();
  const oldestFirst = (x: Res, y: Res) => (x.createdAt ?? x.firstSeenAt).getTime() - (y.createdAt ?? y.firstSeenAt).getTime();

  const open = live.filter((r) => r.open.length).sort(bySeverity);
  const cantTell = live.filter((r) => !r.open.length && r.cantTell.length).sort(bySeverity);
  const waiting = live
    .filter((r) => r.review?.state === "pending" && r.review.overdueDays >= 0)
    .sort((x, y) => (y.review?.overdueDays ?? 0) - (x.review?.overdueDays ?? 0));
  const comingUp = live
    .filter((r) => r.review?.state === "pending" && r.review.overdueDays < 0)
    .sort((x, y) => (x.review?.dueAt.getTime() ?? 0) - (y.review?.dueAt.getTime() ?? 0));
  const decided = resources
    .filter((r) => r.review && r.review.state !== "pending")
    .sort((x, y) => (y.review?.decidedAt?.getTime() ?? 0) - (x.review?.decidedAt?.getTime() ?? 0));
  const unknownOrigin = live.filter((r) => r.origin === "unknown").sort(oldestFirst);
  const agentMade = live.filter((r) => r.origin === "agent").sort(oldestFirst);
  const gone = resources.filter((r) => r.existence === "gone");

  const projectMap = new Map<string, Project>();
  for (const r of resources) {
    const p = projectMap.get(r.projectId) ?? { id: r.projectId, name: r.project, provider: r.provider, account: r.account, resources: [], open: 0, waiting: 0 };
    p.resources.push(r);
    if (r.status === "open") p.open++;
    if (r.status === "waiting") p.waiting++;
    projectMap.set(r.projectId, p);
  }
  const projects = [...projectMap.values()].sort((x, y) => y.open - x.open || y.waiting - x.waiting || x.name.localeCompare(y.name));

  const groups = new Map<string, CheckGroup>();
  for (const c of a.checks) {
    const key = `${c.provider}:${c.account_id}`;
    const g = groups.get(key) ?? { provider: c.provider, account: accountName(a.resources, c), accountId: c.account_id, checks: [], ok: true, failing: [], lastSuccessAt: undefined };
    g.checks.push(c);
    if (c.status === "error") {
      g.ok = false;
      g.failing.push(c);
    }
    const ls = date(c.last_success_at);
    if (ls && (!g.lastSuccessAt || ls > g.lastSuccessAt)) g.lastSuccessAt = ls;
    groups.set(key, g);
  }
  const checks = [...groups.values()].sort((x, y) => x.provider.localeCompare(y.provider));
  const checkedAt = checks.reduce<Date | undefined>((acc, g) => (g.lastSuccessAt && (!acc || g.lastSuccessAt > acc) ? g.lastSuccessAt : acc), undefined);

  const hasProviders = checks.length > 0;
  const state: OverviewState = !hasProviders && sessions.length === 0 && resources.length === 0
    ? "new"
    : sessions.length === 0
      ? "waiting"
      : open.length
        ? "attention"
        : "clear";

  return {
    now,
    developer: a.developer,
    resources,
    sessions,
    open,
    cantTell,
    waiting,
    comingUp,
    decided,
    unknownOrigin,
    gone,
    agentMade,
    projects,
    checks,
    warnings: a.warnings,
    checkedAt,
    state,
    hasProviders,
  };
}

function accountName(resources: Resource[], c: ProviderCheck): string {
  const r = resources.find((x) => x.provider === c.provider && x.account_id === c.account_id && x.account_name);
  return r?.account_name ?? c.account_id;
}

export function resourceById(m: Model, id: string): Res | undefined {
  return m.resources.find((r) => r.id === id);
}

export function sessionById(m: Model, id: string): Ses | undefined {
  return m.sessions.find((s) => s.id === id || s.short === id);
}

export function sessionResources(m: Model, s: Ses): Res[] {
  return s.resourceIds.map((id) => resourceById(m, id)).filter((r): r is Res => !!r);
}

const WARNING_HINT: Record<string, string> = {
  hooks_not_installed: "Sessions there are read from transcripts only, which can miss what an agent made.",
  hooks_disabled: "Sessions there are read from transcripts only, which can miss what an agent made.",
  hooks_managed_only: "Your organization's settings block the hooks, so sessions are read from transcripts only.",
  no_providers: "Connect Supabase or Vercel so Muretto can see what your agents made.",
  transcript_only_sessions: "Some sessions were read from transcripts only, which can miss what an agent made.",
};

/** Problems with the user's setup, most important first. */
export interface Problem {
  code: string;
  title: string;
  text: string;
  command?: string;
  href?: string;
}

export function problems(m: Model): Problem[] {
  const out: Problem[] = [];
  for (const g of m.checks) {
    for (const c of g.failing) {
      const name = g.provider === "supabase" ? "Supabase" : g.provider === "vercel" ? "Vercel" : g.provider;
      if (c.error_code === "auth") {
        out.push({ code: "check_failed", title: `${name} rejected your token`, text: `${name} results are from your last good check.`, command: "muretto init" });
      } else if (c.error_code === "permission") {
        out.push({ code: "check_failed", title: `${name} doesn't allow reading ${c.kind.split(".").pop()?.replace(/_/g, " ")}s`, text: "Give the token read access to them, then check again.", command: "muretto init" });
      } else if (c.error_code === "rate_limit") {
        out.push({ code: "check_failed", title: `${name} asked Muretto to slow down`, text: "It will check again by itself." });
      } else {
        out.push({ code: "check_failed", title: `Couldn't check ${name}`, text: c.error ?? "The provider didn't answer.", command: "muretto doctor" });
      }
    }
  }
  for (const w of m.warnings) {
    if (w.code === "check_failed") continue;
    const command = w.hint?.startsWith("muretto ") ? w.hint : undefined;
    out.push({ code: w.code, title: w.message, text: command ? WARNING_HINT[w.code] ?? "" : w.hint ?? "", command });
  }
  const seen = new Set<string>();
  return out.filter((p) => (seen.has(p.title) ? false : (seen.add(p.title), true)));
}
