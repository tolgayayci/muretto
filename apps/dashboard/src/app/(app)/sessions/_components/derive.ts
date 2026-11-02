// Everything the session page shows, derived from the Model only. Nothing here
// invents data: calls are the evidence (the tool calls that created
// something), providers are the ones those resources live in.
import { clock, day, when } from "@/lib/format";
import type { Model, Res, Ses } from "@/lib/model";

/** Ids are UUIDs or the 4-char short id; anything else can't be a session. */
export function cleanId(raw: string): string | undefined {
  let id: string;
  try {
    id = decodeURIComponent(raw).trim();
  } catch {
    return undefined;
  }
  return /^[A-Za-z0-9-]{1,64}$/.test(id) ? id : undefined;
}

/** "today 14:02" or "Oct 29 18:40". */
export function stamp(d: Date, now: Date): string {
  const w = when(d, now);
  return w.startsWith("today ") ? w : `${day(d)} ${clock(d)}`;
}

/** "14:04" today, "Oct 29 18:55" otherwise. */
export function shortStamp(d: Date, now: Date): string {
  const w = when(d, now);
  return w.startsWith("today ") ? clock(d) : `${day(d)} ${clock(d)}`;
}

const CLIS = new Set(["supabase", "vercel"]);

/** "mcp__supabase__apply_migration" → apply_migration via Supabase MCP; Bash running the vercel CLI → Bash via vercel CLI. */
export function toolLabel(tool: string, excerpt: string): { tool: string; via: string; short: string } {
  const mcp = /^mcp__([^_]+(?:_[^_]+)*?)__(.+)$/.exec(tool);
  if (mcp) {
    const server = mcp[1].replace(/[-_]/g, " ");
    const name = server.charAt(0).toUpperCase() + server.slice(1);
    return { tool: mcp[2], via: `${name} MCP`, short: `MCP ${mcp[2]}` };
  }
  const first = excerpt.trim().split(/\s+/)[0]?.toLowerCase() ?? "";
  if (CLIS.has(first)) return { tool, via: `${first} CLI`, short: `${first} CLI` };
  return { tool, via: "", short: tool };
}

export interface Call {
  id: string;
  at: Date;
  tool: string;
  via: string;
  short: string;
  excerpt: string;
  made: Res[];
}

/** One entry per tool call that created something, oldest first. */
export function creatingCalls(resources: Res[]): Call[] {
  const byId = new Map<string, Call>();
  for (const r of resources) {
    for (const e of r.evidence) {
      const at = new Date(e.occurred_at);
      if (Number.isNaN(at.getTime())) continue;
      const c = byId.get(e.action_id);
      if (c) {
        if (!c.made.includes(r)) c.made.push(r);
        continue;
      }
      byId.set(e.action_id, { id: e.action_id, at, ...toolLabel(e.tool_name, e.excerpt), excerpt: e.excerpt, made: [r] });
    }
  }
  return [...byId.values()].sort((a, b) => a.at.getTime() - b.at.getTime());
}

/** The call that made a resource, if recorded. */
export function madeBy(r: Res): { at: Date; short: string } | undefined {
  const e = [...r.evidence].sort((a, b) => a.occurred_at.localeCompare(b.occurred_at))[0];
  if (!e) return undefined;
  const at = new Date(e.occurred_at);
  if (Number.isNaN(at.getTime())) return undefined;
  const t = toolLabel(e.tool_name, e.excerpt);
  // "vercel deploy" reads better than "Bash" for a CLI call.
  const short = t.via.endsWith(" CLI") ? e.excerpt.trim().split(/\s+/).slice(0, 2).join(" ") : t.tool;
  return { at, short };
}

/** Made after the newest successful provider check: Muretto hasn't looked at it yet. */
export function notCheckedYet(r: Res, m: Model): boolean {
  if (r.status === "gone" || r.findings.length) return false;
  const born = r.createdAt ?? r.firstSeenAt;
  return !m.checkedAt || born.getTime() > m.checkedAt.getTime();
}

const RULE_HINT: Record<string, string> = {
  rls_disabled: "row level security off",
  public_bucket: "public",
  public_preview: "protection off",
  client_exposed_env: "sent to every browser",
};

/** "table · row level security off". */
export function rowSub(r: Res, m: Model): string {
  if (r.status === "gone") return `${r.kind} · gone ${r.goneAt ? day(r.goneAt) : ""}`.trim();
  if (r.open.length) return `${r.kind} · ${RULE_HINT[r.open[0].ruleId] ?? r.open[0].title}`;
  if (r.cantTell.length || r.status === "unconfirmed") return `${r.kind} · Muretto couldn't check it`;
  if (notCheckedYet(r, m)) return `${r.kind} · not checked yet`;
  return r.kind;
}

export interface ProviderUse {
  provider: string;
  accounts: string[];
  projects: string[];
  made: number;
  open: number;
  still: number;
  /** Tool labels of the calls that created something there, with counts. */
  tools: [string, number][];
}

export function providersUsed(resources: Res[]): ProviderUse[] {
  const map = new Map<string, { rs: Res[] }>();
  for (const r of resources) {
    const p = map.get(r.provider) ?? { rs: [] };
    p.rs.push(r);
    map.set(r.provider, p);
  }
  return [...map.entries()]
    .map(([provider, { rs }]) => {
      const tools = new Map<string, number>();
      for (const c of creatingCalls(rs)) tools.set(c.short, (tools.get(c.short) ?? 0) + 1);
      return {
        provider,
        accounts: [...new Set(rs.map((r) => r.account))],
        projects: [...new Set(rs.map((r) => r.project))],
        made: rs.length,
        open: rs.filter((r) => r.status === "open").length,
        still: rs.filter((r) => r.status !== "gone").length,
        tools: [...tools.entries()].sort((a, b) => b[1] - a[1]),
      };
    })
    .sort((a, b) => b.made - a.made);
}

export interface Group {
  key: string;
  provider: string;
  project: string;
  account: string;
  rows: Res[];
}

/** Resources grouped by provider and project, groups with problems first. */
export function groupByProject(resources: Res[]): Group[] {
  const map = new Map<string, Group>();
  for (const r of resources) {
    const key = `${r.provider}:${r.projectId}`;
    const g = map.get(key) ?? { key, provider: r.provider, project: r.project, account: r.account, rows: [] };
    g.rows.push(r);
    map.set(key, g);
  }
  const born = (r: Res) => (r.createdAt ?? r.firstSeenAt).getTime();
  const groups = [...map.values()];
  for (const g of groups) g.rows.sort((a, b) => born(a) - born(b));
  return groups.sort((a, b) => born(a.rows[0]) - born(b.rows[0]));
}

const KIND_PLURAL: Record<string, string> = { branch: "branches", "env var": "env vars" };

/** "4 tables · 1 bucket". */
export function kindSummary(rows: Res[]): string {
  const counts = new Map<string, number>();
  for (const r of rows) counts.set(r.kind, (counts.get(r.kind) ?? 0) + 1);
  return [...counts.entries()].map(([k, n]) => `${n} ${n === 1 ? k : (KIND_PLURAL[k] ?? `${k}s`)}`).join(" · ");
}

export function joinAnd(items: string[]): string {
  if (items.length <= 1) return items[0] ?? "";
  return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

export interface Facts {
  s: Ses;
  rs: Res[];
  open: Res[];
  cantTell: Res[];
  waiting: Res[];
  gone: Res[];
  pending: Res[];
  unchecked: Res[];
}

export function facts(m: Model, s: Ses, rs: Res[]): Facts {
  const live = rs.filter((r) => r.status !== "gone");
  return {
    s,
    rs,
    open: live.filter((r) => r.status === "open"),
    cantTell: live.filter((r) => r.status === "cant_tell" || r.status === "unconfirmed"),
    waiting: live.filter((r) => r.status === "waiting"),
    gone: rs.filter((r) => r.status === "gone"),
    pending: live.filter((r) => r.review?.state === "pending" && r.review.overdueDays < 0),
    unchecked: live.filter((r) => notCheckedYet(r, m)),
  };
}
