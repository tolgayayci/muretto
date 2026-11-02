// Pure helpers for My resources: URL params, filters, and what each row says.
import { day } from "@/lib/format";
import type { CheckGroup, Model, Project, Res } from "@/lib/model";

export const FILTERS = ["all", "open", "waiting", "agent", "unknown", "gone"] as const;
export type Filter = (typeof FILTERS)[number];

const ALIASES: Record<string, Filter> = { agents: "agent", wait: "waiting" };

export const FILTER_LABEL: Record<Filter, string> = {
  all: "All",
  open: "Open to anyone",
  waiting: "Waiting on you",
  agent: "Made by your agents",
  unknown: "Origin unknown",
  gone: "Gone",
};

const FILTER_PHRASE: Record<Filter, string> = {
  all: "",
  open: "open to anyone",
  waiting: "waiting on you",
  agent: "made by your agents",
  unknown: "with origin unknown",
  gone: "gone",
};

export type Raw = Record<string, string | string[] | undefined>;

export interface Params {
  filter: Filter;
  q: string;
  newest: boolean;
  /** Provider id, only when one of the projects is on it. */
  provider: string;
  /** Project name, only when it names a real project (on the chosen provider). */
  project: string;
  /** Project names whose quiet rows are shown (many projects). */
  expand: string[];
}

const first = (v: string | string[] | undefined): string => (Array.isArray(v) ? v[0] ?? "" : v ?? "");

/** Reads the URL params against allowlists; anything unknown falls back to the default. */
export function readParams(raw: Raw, m: Model): Params {
  const f = first(raw.filter).toLowerCase();
  const filter: Filter = (FILTERS as readonly string[]).includes(f) ? (f as Filter) : ALIASES[f] ?? "all";
  const q = first(raw.q).replace(/\s+/g, " ").trim().slice(0, 80);
  const names = new Set(m.projects.map((p) => p.name));
  const provider = m.projects.some((p) => p.provider === first(raw.provider)) ? first(raw.provider) : "";
  const pr = first(raw.project);
  const project = m.projects.some((p) => p.name === pr && (!provider || p.provider === provider)) ? pr : "";
  const ex = raw.expand === undefined ? [] : Array.isArray(raw.expand) ? raw.expand : [raw.expand];
  return { filter, q, newest: first(raw.sort) === "new", provider, project, expand: [...new Set(ex.filter((x) => names.has(x)))].slice(0, 50) };
}

/** Builds a /resources link from params, leaving defaults out. */
export function href(p: Params, change: Partial<Params> = {}): string {
  const n = { ...p, ...change };
  const sp = new URLSearchParams();
  if (n.filter !== "all") sp.set("filter", n.filter);
  if (n.q) sp.set("q", n.q);
  if (n.newest) sp.set("sort", "new");
  if (n.provider) sp.set("provider", n.provider);
  if (n.project) sp.set("project", n.project);
  for (const e of n.expand) sp.append("expand", e);
  const s = sp.toString();
  return s ? `/resources?${s}` : "/resources";
}

export function counts(m: Model): Record<Filter, number> {
  const live = m.resources.filter((r) => r.existence !== "gone");
  return {
    all: live.length,
    open: m.open.length,
    waiting: m.waiting.length,
    agent: m.agentMade.length,
    unknown: m.unknownOrigin.length,
    gone: m.gone.length,
  };
}

const waitingIds = (m: Model) => new Set(m.waiting.map((r) => r.id));

export function passes(r: Res, filter: Filter, waiting: Set<string>): boolean {
  if (filter === "gone") return r.existence === "gone";
  if (r.existence === "gone") return false;
  switch (filter) {
    case "open":
      return r.open.length > 0;
    case "waiting":
      return waiting.has(r.id);
    case "agent":
      return r.origin === "agent";
    case "unknown":
      return r.origin === "unknown";
    default:
      return true;
  }
}

export function hits(r: Res, q: string, m: Model): boolean {
  if (!q) return true;
  const s = q.toLowerCase();
  const ses = r.sessionId ? m.sessions.find((x) => x.id === r.sessionId) : undefined;
  return [r.name, r.project, ses?.short ?? "", ses?.title ?? ""].some((x) => x.toLowerCase().includes(s));
}

const inProject = (pr: Project, p: Pick<Params, "provider" | "project">) => (!p.provider || pr.provider === p.provider) && (!p.project || pr.name === p.project);

export interface Facets {
  /** Per provider: resources matching the status and search, any project. */
  providers: { provider: string; count: number }[];
  /** Projects on the chosen provider (all when none), with resources matching the status and search. */
  projects: { project: Project; count: number }[];
  /** Per status: resources in the chosen provider and project matching the search. */
  status: Record<Filter, number>;
}

/** Counts for the filter menus. Each menu counts what the other filters leave, so they narrow each other. */
export function facets(m: Model, p: Params): Facets {
  const waiting = waitingIds(m);
  const fits = (r: Res) => passes(r, p.filter, waiting) && hits(r, p.q, m);
  const providers = [...new Set(m.projects.map((pr) => pr.provider))].map((provider) => ({
    provider,
    count: m.projects.filter((pr) => pr.provider === provider).reduce((n, pr) => n + pr.resources.filter(fits).length, 0),
  }));
  const projects = m.projects.filter((pr) => inProject(pr, { provider: p.provider, project: "" })).map((project) => ({ project, count: project.resources.filter(fits).length }));
  const scoped = m.projects.filter((pr) => inProject(pr, p)).flatMap((pr) => pr.resources.filter((r) => hits(r, p.q, m)));
  const status = Object.fromEntries(FILTERS.map((f) => [f, scoped.filter((r) => passes(r, f, waiting)).length])) as Record<Filter, number>;
  return { providers, projects, status };
}

const born = (r: Res) => (r.createdAt ?? r.firstSeenAt).getTime();

/** Needs a look: open, can't tell, or waiting on a decision. */
export const needsLook = (r: Res, waiting: Set<string>) => r.open.length > 0 || r.cantTell.length > 0 || waiting.has(r.id);

export interface Group {
  project: Project;
  rows: Res[];
  /** Rows hidden because the group is folded. */
  hidden: number;
  folded: boolean;
  /** Live resources in the project. */
  total: number;
  open: number;
  failing?: CheckGroup;
}

export function failingGroup(m: Model, r: { provider: string; account: string }): CheckGroup | undefined {
  return m.checks.find((g) => !g.ok && g.provider === r.provider && g.account === r.account);
}

/** Groups by project, filtered and sorted. Folding hides quiet rows when there are many projects. */
export function groups(m: Model, p: Params, fold: boolean): Group[] {
  const waiting = waitingIds(m);
  const plain = p.filter === "all" && !p.q && !p.project && !p.provider;
  return m.projects
    .filter((pr) => inProject(pr, p))
    .map((pr) => {
      const rows = pr.resources
        .filter((r) => passes(r, p.filter, waiting) && hits(r, p.q, m))
        .sort((a, b) => (p.newest ? born(b) - born(a) : born(a) - born(b)) || a.name.localeCompare(b.name));
      const folded = fold && plain && !p.expand.includes(pr.name);
      const shown = folded ? rows.filter((r) => needsLook(r, waiting)) : rows;
      return {
        project: pr,
        rows: shown,
        hidden: rows.length - shown.length,
        folded,
        total: pr.resources.filter((r) => r.existence !== "gone").length,
        open: pr.resources.filter((r) => r.existence !== "gone" && r.open.length > 0).length,
        failing: failingGroup(m, pr),
      };
    })
    .filter((g) => g.rows.length > 0 || g.hidden > 0);
}

const RULE_SHORT: Record<string, string> = {
  rls_disabled: "RLS off",
  public_bucket: "public bucket",
  public_preview: "public preview",
  client_exposed_env: "sent to the browser",
};

export function ruleShort(ruleId: string): string | undefined {
  return RULE_SHORT[ruleId];
}

export type Tone = "open" | "wait" | "dash" | "quiet";
export type Action = "details" | "decide" | "link" | "age";

export interface RowStatus {
  tone: Tone;
  label: string;
  /** Colour for quiet labels. */
  color?: string;
  action: Action;
}

export function rowStatus(r: Res, waiting: boolean): RowStatus {
  if (r.existence === "gone") return { tone: "quiet", label: r.goneAt ? `gone ${day(r.goneAt)}` : "gone", action: "age" };
  if (r.open.length) return { tone: "open", label: "Open to anyone", action: "details" };
  if (r.cantTell.length) return { tone: "dash", label: "can't tell", action: "link" };
  if (waiting) return { tone: "wait", label: `Waiting · ${r.ageDays} d`, action: "decide" };
  if (r.existence === "unconfirmed") return { tone: "dash", label: "unconfirmed", action: "age" };
  const rv = r.review;
  if (rv && rv.state !== "pending") {
    const when = rv.decidedAt ? ` ${day(rv.decidedAt)}` : "";
    return rv.state === "keep"
      ? { tone: "quiet", label: `kept${when}`, color: "var(--ink-muted)", action: "age" }
      : { tone: "quiet", label: `not needed${when}`, color: "var(--ink-muted)", action: "age" };
  }
  if (r.findings.some((f) => f.state === "not_exposed")) {
    return { tone: "quiet", label: r.kind === "preview" || r.kind === "deployment" ? "protected" : "private", action: "age" };
  }
  return { tone: "quiet", label: "", action: "age" };
}

/** "today" or "11 d". */
export function shortAge(days: number): string {
  return days <= 0 ? "today" : `${days} d`;
}

export function emptyCopy(p: Params, all: number) {
  const phrase = FILTER_PHRASE[p.filter];
  const title = p.q ? (phrase ? `Nothing ${phrase} matches “${p.q}”.` : `Nothing matches “${p.q}”.`) : `Nothing ${phrase || "here"} right now.`;
  const text = p.q ? "Search looks at resource names, projects and the sessions that made them." : "This list updates after each check.";
  const filters = [p.filter !== "all" ? `showing: ${FILTER_LABEL[p.filter]}` : "", p.provider ? `provider: ${p.provider}` : "", p.project ? `project: ${p.project}` : "", p.q ? `search: “${p.q}”` : ""].filter(Boolean).join(" · ");
  return { title, text, filters, showAll: p.q && p.filter !== "all" ? `Show all ${all}` : p.q && p.filter === "all" && !p.project && !p.provider ? "Clear search" : `Show all ${all} resources` };
}
