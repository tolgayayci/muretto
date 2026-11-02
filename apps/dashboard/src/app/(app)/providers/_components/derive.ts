// What the Providers page shows, derived from the model. Pure functions only.
import type { ProviderCheck } from "@/contract";
import type { CheckGroup, Model } from "@/lib/model";
import { ago } from "@/lib/format";

/** The providers Muretto reads today. Never add one here before its adapter ships. */
export const SUPPORTED = ["supabase", "vercel"] as const;

/** Adapters being built, with what they will list. They move to SUPPORTED when they ship. */
export const COMING: { provider: string; lists: string }[] = [
  { provider: "github", lists: "repos, Pages sites, webhooks, deploy keys, environments, Actions variables, app installations" },
  { provider: "cloudflare", lists: "R2 buckets, Workers, Pages, D1, KV, zones, DNS, tunnels" },
  { provider: "neon", lists: "projects, branches, computes, databases, roles" },
  { provider: "stripe", lists: "webhook endpoints, products, prices, payment links, billing portal configs" },
  { provider: "render", lists: "services, Postgres, Key Value, custom domains, Blueprints" },
  { provider: "planetscale", lists: "databases, branches, passwords, Postgres roles, deploy requests" },
];

export const ENV: Record<string, string> = {
  supabase: "MURETTO_SUPABASE_TOKEN",
  vercel: "MURETTO_VERCEL_TOKEN",
};

export const LISTS: Record<string, string[]> = {
  supabase: ["project", "branch", "table", "bucket", "function"],
  vercel: ["project", "deployment", "env_var", "domain", "project_domain"],
};

const PLURAL: Record<string, string> = {
  project: "projects",
  branch: "branches",
  table: "tables",
  bucket: "buckets",
  function: "functions",
  edge_function: "functions",
  deployment: "deployments",
  env_var: "env vars",
  domain: "domains",
  project_domain: "project domains",
};

/** "supabase.env_var" → "env vars". */
export function kindLabel(kind: string): string {
  const k = kind.includes(".") ? kind.slice(kind.indexOf(".") + 1) : kind;
  return PLURAL[k] ?? `${k.replace(/_/g, " ")}s`;
}

function bare(kind: string): string {
  return kind.includes(".") ? kind.slice(kind.indexOf(".") + 1) : kind;
}

export interface Flag {
  ruleId: string;
  /** Kind the rule runs on, for the icon. */
  icon: string;
  /** Check kind that evaluates the rule. */
  checkKind: string;
  what: string;
  /** Short name for chips. */
  short: string;
  sub: string;
}

export const FLAGS: Record<string, Flag[]> = {
  supabase: [
    { ruleId: "rls_disabled", icon: "table", checkKind: "table", what: "Tables anyone can read", short: "Tables anyone can read", sub: "critical · row level security off" },
    { ruleId: "public_bucket", icon: "bucket", checkKind: "bucket", what: "Buckets anyone can download from", short: "Public buckets", sub: "high · public storage bucket" },
  ],
  vercel: [
    { ruleId: "public_preview", icon: "preview", checkKind: "deployment", what: "Previews anyone can open", short: "Open previews", sub: "high · deployment protection off" },
    { ruleId: "client_exposed_env", icon: "env var", checkKind: "env_var", what: "Secrets sent to the browser", short: "Secrets in the browser", sub: "high · env var names only, never values" },
  ],
};

export type CellState = "ok" | "old" | "retry" | "denied" | "failed" | "skipped";

export interface KindCell {
  kind: string;
  label: string;
  /** Items listed; undefined when the provider never answered. */
  items?: number;
  state: CellState;
  /** "3 min ago", "2 days old", "no access", "retrying", "can't check". */
  note: string;
}

/** One cell per kind, scopes of the same kind added up. */
export function kindCells(g: CheckGroup, now: Date): KindCell[] {
  const byKind = new Map<string, ProviderCheck[]>();
  for (const c of g.checks) {
    const list = byKind.get(c.kind) ?? [];
    list.push(c);
    byKind.set(c.kind, list);
  }
  const order = LISTS[g.provider] ?? [];
  const rank = (k: string) => {
    const i = order.indexOf(bare(k));
    return i < 0 ? order.length : i;
  };
  return [...byKind.entries()]
    .sort(([a], [b]) => rank(a) - rank(b))
    .map(([kind, cs]) => {
      const failed = cs.filter((c) => c.status === "error");
      const okAll = cs.every((c) => c.status === "ok");
      const anyItems = cs.some((c) => typeof c.items === "number");
      const items = anyItems ? cs.reduce((n, c) => n + (c.items ?? 0), 0) : undefined;
      const successes = cs.map((c) => (c.last_success_at ? new Date(c.last_success_at) : undefined)).filter((d): d is Date => !!d && !Number.isNaN(d.getTime()));
      const oldest = successes.sort((a, b) => a.getTime() - b.getTime())[0];
      let state: CellState;
      let note: string;
      if (okAll) {
        state = "ok";
        note = oldest ? ago(oldest, now) : "checked";
      } else if (!failed.length) {
        state = "skipped";
        note = "skipped";
      } else {
        const code = failed[0].error_code;
        if (code === "rate_limit") {
          state = "retry";
          note = "retrying";
        } else if (code === "permission") {
          state = "denied";
          note = "no access";
        } else if (oldest) {
          state = "old";
          note = `${ago(oldest, now).replace(" ago", "")} old`;
        } else {
          state = "failed";
          note = "can't check";
        }
      }
      return { kind, label: kindLabel(kind), items: state === "denied" || (state === "failed" && !oldest) ? undefined : items, state, note };
    });
}

export type FlagResult = { kind: "open"; count: number } | { kind: "none" } | { kind: "unknown" };

export function flagResult(m: Model, g: CheckGroup, f: Flag): FlagResult {
  const mine = m.resources.filter((r) => r.provider === g.provider && r.account === g.account && r.existence !== "gone");
  const open = mine.filter((r) => r.open.some((x) => x.ruleId === f.ruleId)).length;
  if (open) return { kind: "open", count: open };
  const unknown = mine.some((r) => r.cantTell.some((x) => x.ruleId === f.ruleId));
  const checks = g.checks.filter((c) => bare(c.kind) === f.checkKind);
  if (unknown || !checks.length || checks.some((c) => c.status !== "ok")) return { kind: "unknown" };
  return { kind: "none" };
}

/** Number of projects the account's project check listed. */
export function projectCount(g: CheckGroup): number | undefined {
  const cs = g.checks.filter((c) => bare(c.kind) === "project" && typeof c.items === "number");
  return cs.length ? cs.reduce((n, c) => n + (c.items ?? 0), 0) : undefined;
}

export type GroupTone = "ok" | "partial" | "down";

export function groupTone(g: CheckGroup): GroupTone {
  if (g.ok) return "ok";
  return g.failing.length === g.checks.filter((c) => c.status !== "skipped").length ? "down" : "partial";
}

export interface CheckProblem {
  key: string;
  icon: "key" | "lock" | "clock" | "wifioff" | "question";
  title: string;
  text: string;
  /** "acme · auth · "invalid token" · tried 3 min ago". */
  code: string;
  command?: string;
  /** Shown instead of a command when Muretto retries by itself. */
  retrying?: boolean;
  checks: number;
}

function list(words: string[]): string {
  if (words.length <= 1) return words[0] ?? "";
  return `${words.slice(0, -1).join(", ")} and ${words[words.length - 1]}`;
}

function cap(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/** Failing checks grouped by account and error, with a plain explanation and what to check. */
export function checkProblems(m: Model, name: (provider: string) => string): CheckProblem[] {
  const out: CheckProblem[] = [];
  const sameProvider = (p: string) => m.checks.filter((g) => g.provider === p).length > 1;
  for (const g of m.checks) {
    const byCode = new Map<string, ProviderCheck[]>();
    for (const c of g.failing) {
      const code = c.error_code ?? "provider";
      byCode.set(code, [...(byCode.get(code) ?? []), c]);
    }
    const n = name(g.provider);
    const who = sameProvider(g.provider) ? `${n} (${g.account})` : n;
    for (const [code, cs] of byCode) {
      const kinds = list([...new Set(cs.map((c) => kindLabel(c.kind)))]);
      const all = cs.length === g.checks.filter((c) => c.status !== "skipped").length;
      const what = all ? `${n} results` : kinds;
      const attempted = cs.map((c) => new Date(c.attempted_at)).filter((d) => !Number.isNaN(d.getTime())).sort((a, b) => b.getTime() - a.getTime())[0];
      const lastGood = cs
        .map((c) => (c.last_success_at ? new Date(c.last_success_at) : undefined))
        .filter((d): d is Date => !!d && !Number.isNaN(d.getTime()))
        .sort((a, b) => a.getTime() - b.getTime())[0];
      const msg = cs.find((c) => c.error)?.error;
      const since = lastGood ? `last good check ${ago(lastGood, m.now)}` : "never checked";
      const tried = attempted ? `tried ${ago(attempted, m.now)}` : "";
      const codeLine = (extra: string) => [g.account, code, extra, tried].filter(Boolean).join(" · ");
      const base = { key: `${g.provider}:${g.accountId}:${code}`, checks: cs.length };
      const stale = lastGood ? ` ${cap(what)} here are from ${ago(lastGood, m.now)}.` : ` Nothing from ${n} could be read yet.`;
      switch (code) {
        case "auth":
          out.push({ ...base, icon: "key", title: `${who} rejected your token`, text: `Get a new read-only token, then connect again.${stale}`, code: codeLine(msg ? `"${msg}"` : ""), command: "muretto init" });
          break;
        case "permission":
          out.push({ ...base, icon: "lock", title: `${who} ${kinds} can't be checked`, text: `The token isn't allowed to read ${kinds}. Give it read access, then connect again.`, code: codeLine(msg ? `"${msg}"` : ""), command: "muretto init" });
          break;
        case "rate_limit":
          out.push({ ...base, icon: "clock", title: `${who} ${kinds}: too many requests`, text: `${n} asked Muretto to slow down. It tries again by itself. Nothing to do.`, code: [g.account, code, since].join(" · "), retrying: true });
          break;
        case "network":
          out.push({ ...base, icon: "wifioff", title: `Couldn't reach ${who}`, text: `The machine couldn't reach ${n}. Muretto tries again on the next check.${stale}`, code: codeLine(""), command: "muretto providers --coverage" });
          break;
        case "not_found":
          out.push({ ...base, icon: "question", title: `${who} ${kinds} weren't found`, text: "The token may point at another account, or the project was removed. Check the token, then connect again.", code: codeLine(msg ? `"${msg}"` : ""), command: "muretto init" });
          break;
        case "unsupported":
          out.push({ ...base, icon: "question", title: `${who} ${kinds} can't be read yet`, text: `Muretto can't read ${kinds} for this kind of ${n} account.`, code: codeLine(""), command: "muretto providers --coverage" });
          break;
        default:
          out.push({ ...base, icon: "question", title: `${who} had a problem`, text: `${n} answered with an error. Muretto tries again on the next check.${stale}`, code: codeLine(msg ? `"${msg}"` : ""), retrying: true });
      }
    }
  }
  return out;
}
