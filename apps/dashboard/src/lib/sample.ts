// Sample Answers for development, tests and the page-by-page design check.
//
// Everything here is made up: the user alice, the "acme" accounts, every id,
// ref, URL and hash. Ids are shaped like real ones but spelled to be obviously
// fake. One scenario per dashboard state, all typed by the generated contract.
//
// The clock is pinned (SAMPLE_NOW) so every render shows the same text.
import type {
  Answer,
  Attribution,
  Evidence,
  ExposureFinding,
  ProviderCheck,
  Resource,
  ReviewItem,
  Session,
  SessionView,
  Warning,
} from "@/contract";

export const SAMPLE_NOW = new Date("2026-11-01T14:40:00Z");
export const SCENARIOS = ["normal", "new", "waiting", "clear", "many", "problem"] as const;
export type Scenario = (typeof SCENARIOS)[number];

const iso = (s: string) => new Date(s).toISOString().replace(".000Z", "Z");
const minutesAgo = (m: number) => iso(new Date(SAMPLE_NOW.getTime() - m * 60_000).toISOString());
const daysAfter = (s: string, d: number) => iso(new Date(new Date(s).getTime() + d * 86_400_000).toISOString());

const developer = { id: "dev_alice_sample", display_name: "Alice Martin", machine_id_hash: "a11ce00000000000", account_login: "alice" };

// Codex is not recorded by the CLI yet; the sample shows how it will look.
type Agent = "claude-code" | "codex";

interface SessionSpec {
  id: string;
  title: string;
  start: string;
  end?: string;
  agent?: Agent;
  repo?: string;
  branch?: string;
  transcriptOnly?: boolean;
  live?: boolean;
}

function sess(s: SessionSpec): Session {
  return {
    id: s.id,
    agent: (s.agent ?? "claude-code") as Session["agent"],
    agent_version: s.agent === "codex" ? "0.9.2" : "2.1.4",
    developer_id: developer.id,
    repo: { path_hash: "acacacacacacacac", display_name: s.repo ?? "acme/web" },
    git_branch: s.branch ?? "main",
    started_at: s.start,
    ended_at: s.live ? undefined : s.end,
    last_activity_at: s.live ? minutesAgo(1) : s.end,
    task_title: s.title,
    title_source: "first_prompt",
    sources: s.transcriptOnly ? ["transcript"] : ["hook", "transcript"],
  };
}

const SB = { provider: "supabase", account_id: "acme-org-sample", account_name: "Acme org" };
const VC = { provider: "vercel", account_id: "team_acmeSample00000000000", account_name: "acme" };
const SB_PERSONAL = { provider: "supabase", account_id: "alice-personal-sample", account_name: "personal" };
const VC_PERSONAL = { provider: "vercel", account_id: "team_alicePersonalSample0000", account_name: "personal" };

interface ResSpec {
  acct: typeof SB;
  kind: string;
  pid: string;
  name: string;
  parent?: string;
  created?: string;
  gone?: string;
  unconfirmed?: boolean;
  url?: string;
  attrs?: Resource["attributes"];
}

function res(r: ResSpec): Resource {
  const kind = `${r.acct.provider}.${r.kind}`;
  const created = r.created ?? "2026-10-01T09:00:00Z";
  return {
    id: `${kind}:${r.pid}`,
    provider: r.acct.provider,
    account_id: r.acct.account_id,
    account_name: r.acct.account_name,
    kind,
    provider_id: r.pid,
    name: r.name,
    parent_id: r.parent,
    url: r.url,
    created_at: created,
    first_seen_at: daysAfter(created, 0),
    last_seen_at: r.gone ?? minutesAgo(3),
    existence: r.gone ? "gone" : r.unconfirmed ? "unconfirmed" : "confirmed",
    gone_at: r.gone,
    seen_via: r.unconfirmed ? "evidence" : "list",
    attributes: r.attrs ?? {},
  };
}

function ev(sessionId: string, at: string, tool: string, excerpt: string, matched: string): Evidence {
  return { action_id: `toolu_${sessionId.slice(0, 4)}${at.slice(11, 16).replace(":", "")}`, session_id: sessionId, occurred_at: at, tool_name: tool, signature_id: "sample", matched_value: matched, excerpt };
}

function att(r: Resource, session?: Session, evidence: Evidence[] = []): Attribution {
  if (!session) return { resource_id: r.id, label: "unknown", reason: "no recorded agent session created it", evidence: [], computed_at: minutesAgo(3) };
  return { resource_id: r.id, label: "exact", session_id: session.id, rule_id: "id_in_result", reason: "the agent's own call returned it", evidence, computed_at: minutesAgo(3) };
}

const FIX: Record<string, ExposureFinding["fix"]> = {
  rls_disabled: { kind: "sql", text: "alter table public.{name} enable row level security;", note: "Then add a policy for the rows your app should read." },
  public_bucket: { kind: "sql", text: "update storage.buckets set public = false where id = '{name}';", note: "Serve files with signed URLs instead." },
  public_preview: { kind: "steps", text: "Vercel → Project → Settings → Deployment Protection → turn on Vercel Authentication for previews." },
  client_exposed_env: { kind: "steps", text: "Rename the variable without the NEXT_PUBLIC_ prefix and read it only on the server, then rotate the key." },
};
const TITLE: Record<string, string> = {
  rls_disabled: "RLS off: public.{name} is publicly readable",
  public_bucket: "Public bucket: {name}",
  public_preview: "Public preview: {name}",
  client_exposed_env: "Secret-looking env var exposed to the browser: {name}",
};
const SEVERITY: Record<string, ExposureFinding["severity"]> = { rls_disabled: "critical", public_bucket: "high", public_preview: "high", client_exposed_env: "high" };

function find(r: Resource, rule: string, state: ExposureFinding["state"] = "exposed", checkedAt = minutesAgo(3)): ExposureFinding {
  const fill = (s: string) => s.replaceAll("{name}", r.name);
  return {
    id: `${rule}:${r.id}`,
    resource_id: r.id,
    rule_id: rule,
    state,
    severity: SEVERITY[rule],
    title: fill(TITLE[rule]),
    explanation: fill(TITLE[rule]),
    fix: { ...FIX[rule], text: fill(FIX[rule].text) },
    checked_at: state === "unknown" ? undefined : checkedAt,
    signal: rule === "rls_disabled" ? "official_advisor" : "api_field",
  };
}

function rev(r: Resource, state: ReviewItem["state"] = "pending", decidedAt?: string): ReviewItem {
  return { resource_id: r.id, owner_developer_id: developer.id, due_at: daysAfter(r.created_at ?? r.first_seen_at, 7), state, decided_at: decidedAt, decided_by: decidedAt ? developer.id : undefined };
}

function checks(acct: typeof SB, kinds: string[], opts: { error?: ProviderCheck["error_code"]; lastGood?: string } = {}): ProviderCheck[] {
  return kinds.map((k) => ({
    provider: acct.provider,
    account_id: acct.account_id,
    kind: `${acct.provider}.${k}`,
    status: opts.error ? "error" : "ok",
    attempted_at: minutesAgo(3),
    last_success_at: opts.error ? opts.lastGood : minutesAgo(3),
    items: 3,
    error_code: opts.error,
    error: opts.error === "auth" ? "invalid token" : undefined,
  }));
}

const SB_KINDS = ["project", "branch", "table", "bucket", "function"];
const VC_KINDS = ["project", "deployment", "env_var", "domain"];

interface Built {
  sessions: Session[];
  resources: Resource[];
  attributions: Attribution[];
  findings: ExposureFinding[];
  reviews: ReviewItem[];
  checks: ProviderCheck[];
  warnings: Warning[];
}

function answer(b: Built): Answer {
  const made = new Map<string, string[]>();
  for (const a of b.attributions) if (a.session_id) made.set(a.session_id, [...(made.get(a.session_id) ?? []), a.resource_id]);
  const exposed = new Set(b.findings.filter((f) => f.state === "exposed").map((f) => f.resource_id));
  const sessions: SessionView[] = b.sessions.map((s) => ({
    session: s,
    developer_name: developer.display_name,
    resource_ids: made.get(s.id) ?? [],
    exposed_count: (made.get(s.id) ?? []).filter((id) => exposed.has(id)).length,
    review_due_count: 0,
  }));
  const live = b.resources.filter((r) => r.existence !== "gone");
  return {
    schema_version: 1,
    generated_at: iso(SAMPLE_NOW.toISOString()),
    developer,
    summary: {
      sessions: b.sessions.length,
      agent_created: b.attributions.filter((a) => a.label === "exact").length,
      still_existing: live.length,
      exposed: exposed.size,
      needs_review: b.reviews.filter((r) => r.state === "pending" && new Date(r.due_at) <= SAMPLE_NOW).length,
      unknown_creator: b.attributions.filter((a) => a.label === "unknown").length,
      checks_ok: b.checks.filter((c) => c.status === "ok").length,
      checks_failed: b.checks.filter((c) => c.status === "error").length,
    },
    sessions,
    resources: b.resources,
    attributions: b.attributions,
    findings: b.findings,
    costs: [],
    reviews: b.reviews,
    checks: b.checks,
    warnings: b.warnings,
  };
}

// ------------------------------------------------------------ the main story

function story(): Built {
  const S = {
    s8f2a: sess({ id: "8f2a61d3-0000-4000-8000-00000000a11c", title: "add customer import with file uploads", start: "2026-11-01T14:02:00Z", end: "2026-11-01T14:31:00Z", branch: "feat/customer-import" }),
    se7e0: sess({ id: "e7e0b2c4-0000-4000-8000-00000000a11c", title: "look around the projects", start: "2026-11-01T09:00:00Z", end: "2026-11-01T09:04:00Z" }),
    s91bd: sess({ id: "91bd7e20-0000-4000-8000-00000000a11c", title: "send welcome emails", start: "2026-10-29T18:40:00Z", end: "2026-10-29T19:02:00Z", agent: "codex", branch: "feat/welcome" }),
    s7a0c: sess({ id: "7a0c4f88-0000-4000-8000-00000000a11c", title: "track signup events", start: "2026-10-24T16:10:00Z", end: "2026-10-24T16:31:00Z", transcriptOnly: true, branch: "feat/events" }),
    sc2e8: sess({ id: "c2e8d913-0000-4000-8000-00000000a11c", title: "pricing page previews", start: "2026-10-23T11:20:00Z", end: "2026-10-23T11:52:00Z", branch: "pricing" }),
    s0f19: sess({ id: "0f19aa52-0000-4000-8000-00000000a11c", title: "set up the database", start: "2026-10-21T10:00:00Z", end: "2026-10-21T10:18:00Z", agent: "codex" }),
  };
  const DEV = "acmedevsampleref0000a";
  const PROD = "acmeprodsampleref000b";
  const WEB = "prj_acmeWebSample000000000001";
  const R = {
    acmeDev: res({ acct: SB, kind: "project", pid: DEV, name: "acme-dev", created: "2026-10-21T10:06:00Z" }),
    acmeProd: res({ acct: SB, kind: "project", pid: PROD, name: "acme-prod", created: "2026-10-12T08:00:00Z" }),
    legacyUsers: res({ acct: SB, kind: "table", pid: `${PROD}/public.legacy_users`, name: "legacy_users", parent: `supabase.project:${PROD}`, created: "2026-10-12T08:10:00Z" }),
    events: res({ acct: SB, kind: "table", pid: `${DEV}/public.events`, name: "events", parent: `supabase.project:${DEV}`, created: "2026-10-24T16:22:00Z" }),
    sendWelcome: res({ acct: SB, kind: "function", pid: `${DEV}/send-welcome`, name: "send-welcome", parent: `supabase.project:${DEV}`, created: "2026-10-29T18:55:00Z" }),
    customers: res({ acct: SB, kind: "table", pid: `${DEV}/public.customers`, name: "customers", parent: `supabase.project:${DEV}`, created: "2026-11-01T14:04:00Z" }),
    customerFiles: res({ acct: SB, kind: "bucket", pid: `${DEV}/customer-files`, name: "customer-files", parent: `supabase.project:${DEV}`, created: "2026-11-01T14:09:00Z", attrs: { public: true } }),
    processImport: res({ acct: SB, kind: "function", pid: `${DEV}/process-import`, name: "process-import", parent: `supabase.project:${DEV}`, created: "2026-11-01T14:15:00Z" }),
    importTest: res({ acct: SB, kind: "branch", pid: "acmeimporttestbranch", name: "import-test", parent: `supabase.project:${DEV}`, created: "2026-11-01T14:21:00Z" }),
    webProject: res({ acct: VC, kind: "project", pid: WEB, name: "acme-web", created: "2026-09-02T09:00:00Z" }),
    previewA81f: res({ acct: VC, kind: "deployment", pid: "dpl_previewA81fSample000000", name: "preview-a81f", parent: `vercel.project:${WEB}`, url: "https://preview-a81f.example.com", created: "2026-11-01T14:27:00Z", attrs: { target: "preview" } }),
    preview77aa: res({ acct: VC, kind: "deployment", pid: "dpl_preview77aaSample000000", name: "preview-77aa", parent: `vercel.project:${WEB}`, created: "2026-10-23T11:40:00Z", attrs: { target: "preview" } }),
    preview1c0e: res({ acct: VC, kind: "deployment", pid: "dpl_preview1c0eSample000000", name: "preview-1c0e", parent: `vercel.project:${WEB}`, created: "2026-10-23T11:48:00Z", gone: "2026-10-25T09:00:00Z", attrs: { target: "preview" } }),
  };
  const s = S.s8f2a.id;
  const attributions = [
    att(R.acmeDev, S.s0f19, [ev(S.s0f19.id, "2026-10-21T10:06:00Z", "mcp__supabase__create_project", '{ "name": "acme-dev", "region": "eu-central-1" }', "acme-dev")]),
    att(R.acmeProd),
    att(R.legacyUsers),
    att(R.events, S.s7a0c, [ev(S.s7a0c.id, "2026-10-24T16:22:00Z", "mcp__supabase__apply_migration", "create table public.events (id bigint generated always as identity, name text, …)", "events")]),
    att(R.sendWelcome, S.s91bd, [ev(S.s91bd.id, "2026-10-29T18:55:00Z", "Bash", "supabase functions deploy send-welcome --project-ref [redacted]", "send-welcome")]),
    att(R.customers, S.s8f2a, [ev(s, "2026-11-01T14:04:00Z", "mcp__supabase__apply_migration", "create table public.customers (id uuid primary key, email text, name text, …)", "customers")]),
    att(R.customerFiles, S.s8f2a, [ev(s, "2026-11-01T14:09:00Z", "mcp__supabase__execute_sql", "insert into storage.buckets (id, name, public) values ('customer-files', 'customer-files', true)", "customer-files")]),
    att(R.processImport, S.s8f2a, [ev(s, "2026-11-01T14:15:00Z", "Bash", "supabase functions deploy process-import --project-ref [redacted]", "process-import")]),
    att(R.importTest, S.s8f2a, [ev(s, "2026-11-01T14:21:00Z", "mcp__supabase__create_branch", '{ "name": "import-test" }', "import-test")]),
    att(R.webProject),
    att(R.previewA81f, S.s8f2a, [ev(s, "2026-11-01T14:27:00Z", "Bash", "vercel deploy --token [redacted]", "preview-a81f")]),
    att(R.preview77aa, S.sc2e8, [ev(S.sc2e8.id, "2026-10-23T11:40:00Z", "Bash", "vercel deploy --token [redacted]", "preview-77aa")]),
    att(R.preview1c0e, S.sc2e8, [ev(S.sc2e8.id, "2026-10-23T11:48:00Z", "Bash", "vercel deploy --token [redacted]", "preview-1c0e")]),
  ];
  return {
    sessions: Object.values(S),
    resources: Object.values(R),
    attributions,
    findings: [
      find(R.customers, "rls_disabled"),
      find(R.customerFiles, "public_bucket"),
      find(R.previewA81f, "public_preview"),
      find(R.preview77aa, "public_preview", "not_exposed"),
      find(R.legacyUsers, "rls_disabled", "not_exposed"),
    ],
    reviews: [
      rev(R.acmeDev),
      rev(R.preview77aa),
      rev(R.events),
      rev(R.sendWelcome, "keep", "2026-10-30T09:12:00Z"),
      rev(R.customers),
      rev(R.customerFiles),
      rev(R.processImport),
      rev(R.importTest),
      rev(R.previewA81f),
    ],
    checks: [...checks(SB, SB_KINDS), ...checks(VC, VC_KINDS)],
    warnings: [],
  };
}

function waitingStory(): Built {
  const kinds: [string, string][] = [
    ["project", "acme-dev"], ["project", "acme-prod"], ["table", "users"], ["table", "orders"], ["table", "audit_log"], ["table", "legacy_users"],
    ["table", "plans"], ["table", "invoices"], ["bucket", "avatars"], ["function", "send-email"], ["function", "resize-image"], ["function", "stripe-webhook"],
  ];
  const DEV = "acmedevsampleref0000a";
  const resources = kinds.map(([kind, name], i) =>
    res({ acct: SB, kind, pid: kind === "project" ? `${DEV}${i}` : `${DEV}/${name}`, name, parent: kind === "project" ? undefined : `supabase.project:${DEV}0`, created: "2026-11-01T14:36:00Z" }),
  );
  const WEB = "prj_acmeWebSample000000000001";
  resources.push(res({ acct: VC, kind: "project", pid: WEB, name: "acme-web", created: "2026-11-01T14:36:00Z" }));
  resources.push(res({ acct: VC, kind: "deployment", pid: "dpl_productionSample0000000", name: "acme-web-prod", parent: `vercel.project:${WEB}`, created: "2026-11-01T14:36:00Z", attrs: { target: "production" } }));
  return {
    sessions: [],
    resources,
    attributions: resources.map((r) => att(r)),
    findings: resources.filter((r) => r.kind === "supabase.table").map((r) => find(r, "rls_disabled", "not_exposed")),
    reviews: [],
    checks: [...checks(SB, SB_KINDS), ...checks(VC, VC_KINDS)],
    warnings: [],
  };
}

function clearStory(): Built {
  const b = story();
  const exposedIds = new Set(b.findings.filter((f) => f.state === "exposed").map((f) => f.resource_id));
  b.findings = b.findings.map((f) => (exposedIds.has(f.resource_id) ? { ...f, state: "not_exposed" } : f));
  // Only one older resource still waits; the rest were decided.
  b.reviews = b.reviews.map((r) => (r.resource_id.includes("acmedevsampleref0000a") && r.resource_id.startsWith("supabase.project") ? { ...r, state: "keep", decided_at: "2026-10-29T08:00:00Z" } : r));
  b.reviews = b.reviews.map((r) => (r.resource_id.includes("public.events") ? { ...r, state: "keep", decided_at: "2026-10-31T08:00:00Z" } : r));
  return b;
}

function problemStory(): Built {
  const b = story();
  b.checks = [...checks(SB, SB_KINDS), ...checks(VC, VC_KINDS, { error: "auth", lastGood: "2026-10-30T14:30:00Z" })];
  b.findings = b.findings.map((f) => (f.resource_id.startsWith("vercel.") ? { ...f, state: "unknown", checked_at: undefined } : f));
  b.resources = b.resources.map((r) => (r.name === "process-import" ? { ...r, existence: "unconfirmed", seen_via: "evidence" } : r));
  b.warnings = [
    { code: "stale_data", message: "alice's Mac mini hasn't synced in 2 days", fix: "Wake it up, or check that sync is on there." },
    { code: "hooks_not_installed", message: "Claude Code hooks are off on alice's Mac mini", fix: "muretto init" },
  ];
  return b;
}

function manyStory(): Built {
  const b = story();
  const BILL = "billingsampleref0000c";
  const API = "acmeapisampleref0000d";
  const LAND = "prj_acmeLandingSample0000001";
  const TOOLS = "prj_internalToolsSample00001";
  const sD3a1 = sess({ id: "d3a1c0de-0000-4000-8000-00000000a11c", title: "invoice webhooks", start: "2026-11-01T13:40:00Z", end: "2026-11-01T13:58:00Z", agent: "codex", repo: "acme/billing" });
  const s71fa = sess({ id: "71fa0b11-0000-4000-8000-00000000a11c", title: "landing page test variant", start: "2026-11-01T09:12:00Z", end: "2026-11-01T09:40:00Z", agent: "codex", repo: "acme/landing" });
  const s5be0 = sess({ id: "5be0f00d-0000-4000-8000-00000000a11c", title: "admin dashboard prototype", start: "2026-11-01T12:15:00Z", end: "2026-11-01T13:02:00Z", repo: "acme/internal-tools" });
  const s9c44 = sess({ id: "9c44beef-0000-4000-8000-00000000a11c", title: "rate limit the public API", start: "2026-11-01T10:30:00Z", end: "2026-11-01T10:51:00Z", repo: "acme/api" });
  const add = (r: Resource, s?: Session, rule?: string) => {
    b.resources.push(r);
    b.attributions.push(att(r, s, s ? [ev(s.id, r.created_at ?? r.first_seen_at, "Bash", `created ${r.name}`, r.name)] : []));
    if (rule) b.findings.push(find(r, rule));
    if (s) b.reviews.push(rev(r));
  };
  add(res({ acct: SB, kind: "project", pid: BILL, name: "billing", created: "2026-10-20T09:00:00Z" }), sD3a1);
  add(res({ acct: SB, kind: "table", pid: `${BILL}/public.invoices`, name: "invoices", parent: `supabase.project:${BILL}`, created: "2026-11-01T13:44:00Z" }), sD3a1, "rls_disabled");
  add(res({ acct: SB, kind: "bucket", pid: `${BILL}/receipts`, name: "receipts", parent: `supabase.project:${BILL}`, created: "2026-11-01T13:50:00Z" }), sD3a1, "public_bucket");
  add(res({ acct: SB, kind: "table", pid: `${BILL}/public.billing_tmp`, name: "billing_tmp", parent: `supabase.project:${BILL}`, created: "2026-10-22T09:00:00Z" }), sD3a1);
  add(res({ acct: VC, kind: "project", pid: LAND, name: "acme-landing", created: "2026-09-10T09:00:00Z" }));
  add(res({ acct: VC, kind: "deployment", pid: "dpl_preview71faSample000000", name: "preview-71fa", parent: `vercel.project:${LAND}`, created: "2026-11-01T09:30:00Z", attrs: { target: "preview" } }), s71fa, "public_preview");
  add(res({ acct: VC, kind: "env_var", pid: `${LAND}/NEXT_PUBLIC_STRIPE_SECRET`, name: "NEXT_PUBLIC_STRIPE_SECRET", parent: `vercel.project:${LAND}`, created: "2026-11-01T09:35:00Z" }), s71fa, "client_exposed_env");
  add(res({ acct: SB_PERSONAL, kind: "project", pid: API, name: "acme-api", created: "2026-10-15T09:00:00Z" }));
  add(res({ acct: SB_PERSONAL, kind: "function", pid: `${API}/rate-limit`, name: "rate-limit", parent: `supabase.project:${API}`, created: "2026-11-01T10:44:00Z" }), s9c44);
  add(res({ acct: VC_PERSONAL, kind: "project", pid: TOOLS, name: "internal-tools", created: "2026-10-28T09:00:00Z" }));
  add(res({ acct: VC_PERSONAL, kind: "deployment", pid: "dpl_tools5be0Sample00000000", name: "tools-5be0", parent: `vercel.project:${TOOLS}`, created: "2026-11-01T12:50:00Z", attrs: { target: "preview" } }), s5be0);
  b.sessions.push(sD3a1, s71fa, s5be0, s9c44);
  b.checks.push(...checks(SB_PERSONAL, SB_KINDS), ...checks(VC_PERSONAL, VC_KINDS));
  return b;
}

const EMPTY: Built = { sessions: [], resources: [], attributions: [], findings: [], reviews: [], checks: [], warnings: [] };

export function sampleAnswer(scenario: Scenario = "normal"): Answer {
  switch (scenario) {
    case "new":
      return answer(EMPTY);
    case "waiting":
      return answer(waitingStory());
    case "clear":
      return answer(clearStory());
    case "many":
      return answer(manyStory());
    case "problem":
      return answer(problemStory());
    default:
      return answer(story());
  }
}

export function isScenario(v: string | undefined | null): v is Scenario {
  return !!v && (SCENARIOS as readonly string[]).includes(v);
}
