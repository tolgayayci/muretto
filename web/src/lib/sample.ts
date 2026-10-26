// Sample data for development, screenshots and the landing page.
//
// Everything here is fake: the "acme" team, developers alice, bob, carol, dan
// and eve, and every id, ref, URL and hash. Ids are shaped like real ones but
// spelled to be obviously made up. Every object is typed by the generated
// contract (src/contract), so the sample follows the schema exactly.
//
// The clock is pinned: SAMPLE_NOW is the "current time" whenever sample data is
// shown, so server and client render identical text.
import type {
  Answer,
  Attribution,
  CostRecord,
  Developer,
  Evidence,
  ExposureFinding,
  ProviderCheck,
  Resource,
  ReviewItem,
  Session,
  SessionView,
  Summary,
  Warning,
} from "@/contract";
import type { Me } from "./api";

export const SAMPLE_NOW = "2026-10-26T14:40:00Z";
export const SAMPLE_TIME_ZONE = "UTC";
export const SAMPLE_ORG = { id: "org_acme_sample", name: "acme", role: "owner" as const };

const LAST_CHECK = "2026-10-26T14:38:00Z";

// ---------------------------------------------------------------- developers

const dev = (name: string, hash: string): Developer => ({
  id: `dev_${name}_sample`,
  display_name: name,
  machine_id_hash: hash,
});

const alice = dev("alice", "a11ce00000000000");
const bob = dev("bob", "b0b0000000000000");
const carol = dev("carol", "ca70100000000000");
const dan = dev("dan", "da70000000000000");
const eve = dev("eve", "e7e0000000000000");
const developers = [alice, bob, carol, dan, eve];

// ------------------------------------------------------------------ sessions

const repoWeb = { path_hash: "acacacacacacacac", display_name: "acme/web", remote_url: "https://github.com/acme/web" };
const repoLanding = {
  path_hash: "abababababababab",
  display_name: "acme/landing",
  remote_url: "https://github.com/acme/landing",
};

const session = (s: Omit<Session, "agent" | "agent_version" | "title_source" | "sources"> & Partial<Session>): Session => ({
  agent: "claude-code",
  agent_version: "2.4.1",
  title_source: "first_prompt",
  sources: ["hook", "transcript"],
  ...s,
});

const S = {
  aliceOld: session({
    id: "2a9b4c10-0000-4000-8000-00000000a11c",
    developer_id: alice.id,
    repo: repoWeb,
    git_branch: "feat/import",
    started_at: "2026-10-18T10:04:00Z",
    ended_at: "2026-10-18T10:26:00Z",
    task_title: "test the import migration on a database branch",
  }),
  alice: session({
    id: "8f2a61d3-0000-4000-8000-00000000a11c",
    developer_id: alice.id,
    repo: repoWeb,
    git_branch: "feat/import",
    started_at: "2026-10-26T14:02:00Z",
    ended_at: "2026-10-26T14:31:00Z",
    task_title: "add customer import with file uploads",
  }),
  bob: session({
    id: "3c71e2f0-0000-4000-8000-000000000b0b",
    developer_id: bob.id,
    repo: repoWeb,
    git_branch: "feat/waitlist",
    started_at: "2026-10-26T14:05:00Z",
    ended_at: "2026-10-26T14:24:00Z",
    task_title: "add a waitlist signup with a Supabase table and a preview",
  }),
  carol: session({
    id: "b4e95a72-0000-4000-8000-0000000ca701",
    developer_id: carol.id,
    repo: repoWeb,
    git_branch: "feat/product-photos",
    started_at: "2026-10-26T14:06:00Z",
    ended_at: "2026-10-26T14:19:00Z",
    task_title: "add image uploads for product photos",
  }),
  dan: session({
    id: "61d0c8e4-0000-4000-8000-000000000da7",
    developer_id: dan.id,
    repo: repoWeb,
    git_branch: "feat/billing-webhook",
    started_at: "2026-10-26T14:21:00Z",
    last_activity_at: "2026-10-26T14:37:00Z",
    task_title: "add a webhook handler as a Supabase edge function",
  }),
  eve: session({
    id: "e05c19b8-0000-4000-8000-000000000e7e",
    developer_id: eve.id,
    repo: repoLanding,
    git_branch: "staging",
    started_at: "2026-10-26T13:40:00Z",
    ended_at: "2026-10-26T14:12:00Z",
    task_title: "deploy a staging copy of the landing page to Vercel",
    sources: ["transcript"],
  }),
};

// ----------------------------------------------------------------- resources

const SB_ACCOUNT = { account_id: "acme-sample-org", account_name: "acme" };
const VC_ACCOUNT = { account_id: "team_acmeSample0000000000", account_name: "acme" };
const REF = "acmedevsamplerefaaaa";
const BRANCH_REF = "acmeimporttestbranch";
const PRJ_WEB = "prj_acmeWebSample000000000001";
const PRJ_STAGING = "prj_acmeStagingSample00000001";

type ResourceInput = Omit<Resource, "id" | "account_id" | "account_name" | "first_seen_at" | "last_seen_at" | "existence" | "seen_via" | "attributes"> &
  Partial<Resource>;

const sb = (r: ResourceInput): Resource => ({
  id: `${r.kind}:${r.provider_id}`,
  ...SB_ACCOUNT,
  existence: "confirmed",
  seen_via: "list",
  first_seen_at: r.created_at ?? LAST_CHECK,
  last_seen_at: LAST_CHECK,
  attributes: {},
  ...r,
});
const vc = (r: ResourceInput): Resource => ({ ...sb(r), ...VC_ACCOUNT });

const R = {
  project: sb({
    provider: "supabase",
    kind: "supabase.project",
    provider_id: REF,
    name: "acme-dev",
    status: "active",
    created_at: "2026-09-02T09:15:00Z",
    attributes: { region: "eu-central-1", plan: "pro" },
  }),
  branch: sb({
    provider: "supabase",
    kind: "supabase.branch",
    provider_id: BRANCH_REF,
    name: "import-test",
    parent_id: `supabase.project:${REF}`,
    status: "active",
    created_at: "2026-10-18T10:09:00Z",
    attributes: { persistent: false },
  }),
  customers: sb({
    provider: "supabase",
    kind: "supabase.table",
    provider_id: `${REF}/public.customers`,
    name: "customers",
    parent_id: `supabase.project:${REF}`,
    seen_via: "advisor",
    first_seen_at: "2026-10-26T14:10:00Z",
    attributes: { schema: "public" },
  }),
  customerFiles: sb({
    provider: "supabase",
    kind: "supabase.bucket",
    provider_id: `${REF}/customer-files`,
    name: "customer-files",
    parent_id: `supabase.project:${REF}`,
    created_at: "2026-10-26T14:14:00Z",
    attributes: { public: true },
  }),
  processImport: sb({
    provider: "supabase",
    kind: "supabase.function",
    provider_id: `${REF}/process-import`,
    name: "process-import",
    parent_id: `supabase.project:${REF}`,
    status: "active",
    created_at: "2026-10-26T14:22:00Z",
    attributes: { verify_jwt: true, version: 1 },
  }),
  waitlist: sb({
    provider: "supabase",
    kind: "supabase.table",
    provider_id: `${REF}/public.waitlist`,
    name: "waitlist",
    parent_id: `supabase.project:${REF}`,
    existence: "unconfirmed",
    seen_via: "evidence",
    first_seen_at: "2026-10-26T14:09:00Z",
    last_seen_at: "2026-10-26T14:09:00Z",
    attributes: { schema: "public" },
  }),
  productImages: sb({
    provider: "supabase",
    kind: "supabase.bucket",
    provider_id: `${REF}/product-images`,
    name: "product-images",
    parent_id: `supabase.project:${REF}`,
    created_at: "2026-10-26T14:11:00Z",
    attributes: { public: false },
  }),
  stripeWebhook: sb({
    provider: "supabase",
    kind: "supabase.function",
    provider_id: `${REF}/billing-webhook`,
    name: "billing-webhook",
    parent_id: `supabase.project:${REF}`,
    status: "active",
    created_at: "2026-10-26T14:36:00Z",
    first_seen_at: "2026-10-26T14:38:00Z",
    attributes: { verify_jwt: false, version: 1 },
  }),
  avatars: sb({
    provider: "supabase",
    kind: "supabase.bucket",
    provider_id: `${REF}/avatars`,
    name: "avatars",
    parent_id: `supabase.project:${REF}`,
    created_at: "2026-10-13T16:40:00Z",
    attributes: { public: false },
  }),
  webProject: vc({
    provider: "vercel",
    kind: "vercel.project",
    provider_id: PRJ_WEB,
    name: "acme-web",
    created_at: "2026-08-21T11:00:00Z",
    attributes: { framework: "nextjs" },
  }),
  previewA81f: vc({
    provider: "vercel",
    kind: "vercel.deployment",
    provider_id: "dpl_SampleA81f000000000000001",
    name: "preview-a81f",
    parent_id: `vercel.project:${PRJ_WEB}`,
    status: "ready",
    url: "https://acme-web-a81f-sample.vercel.app",
    created_at: "2026-10-26T14:27:00Z",
    attributes: { target: "preview" },
  }),
  waitlistPreview: vc({
    provider: "vercel",
    kind: "vercel.deployment",
    provider_id: "dpl_Sample3c71000000000000001",
    name: "preview-3c71",
    parent_id: `vercel.project:${PRJ_WEB}`,
    status: "ready",
    url: "https://acme-web-3c71-sample.vercel.app",
    created_at: "2026-10-26T14:20:00Z",
    attributes: { target: "preview" },
  }),
  domain: vc({
    provider: "vercel",
    kind: "vercel.domain",
    provider_id: "staging.acme.example",
    name: "staging.acme.example",
    created_at: "2026-09-30T08:00:00Z",
    attributes: { verified: true },
  }),
  stagingProject: vc({
    provider: "vercel",
    kind: "vercel.project",
    provider_id: PRJ_STAGING,
    name: "acme-landing-staging",
    created_at: "2026-10-26T13:44:00Z",
    attributes: { framework: "nextjs" },
  }),
  stagingDeployOld: vc({
    provider: "vercel",
    kind: "vercel.deployment",
    provider_id: "dpl_SampleE05b000000000000001",
    name: "staging-e05b",
    parent_id: `vercel.project:${PRJ_STAGING}`,
    status: "error",
    created_at: "2026-10-26T13:52:00Z",
    existence: "gone",
    gone_at: "2026-10-26T14:08:00Z",
    last_seen_at: "2026-10-26T13:58:00Z",
    attributes: { target: "preview" },
  }),
  stagingDeploy: vc({
    provider: "vercel",
    kind: "vercel.deployment",
    provider_id: "dpl_SampleE05c000000000000001",
    name: "staging-e05c",
    parent_id: `vercel.project:${PRJ_STAGING}`,
    status: "ready",
    url: "https://acme-landing-staging-e05c-sample.vercel.app",
    created_at: "2026-10-26T14:06:00Z",
    attributes: { target: "preview" },
  }),
  stripeEnv: vc({
    provider: "vercel",
    kind: "vercel.env_var",
    provider_id: `${PRJ_STAGING}/NEXT_PUBLIC_STRIPE_SECRET_KEY/preview,production`,
    name: "NEXT_PUBLIC_STRIPE_SECRET_KEY",
    parent_id: `vercel.project:${PRJ_STAGING}`,
    created_at: "2026-10-26T13:49:00Z",
    attributes: { targets: "preview,production", type: "encrypted" },
  }),
};

// -------------------------------------------------------------- attributions

const ev = (
  s: Session,
  n: number,
  occurred_at: string,
  tool_name: string,
  signature_id: string,
  matched_value: string,
  excerpt: string,
): Evidence => ({
  action_id: `toolu_01Sample${s.id.slice(0, 4)}${String(n).padStart(4, "0")}`,
  session_id: s.id,
  occurred_at,
  tool_name,
  signature_id,
  matched_value,
  excerpt,
});

const exact = (r: Resource, s: Session, rule_id: string, reason: string, evidence: Evidence[]): Attribution => ({
  resource_id: r.id,
  label: "exact",
  session_id: s.id,
  rule_id,
  reason,
  evidence,
  computed_at: LAST_CHECK,
});

const unknown = (r: Resource, reason: string): Attribution => ({
  resource_id: r.id,
  label: "unknown",
  reason,
  evidence: [],
  computed_at: LAST_CHECK,
});

const attributions: Attribution[] = [
  exact(R.branch, S.aliceOld, "id_in_result", "The branch's project ref appears in the result of the session's create_branch call.", [
    ev(S.aliceOld, 1, "2026-10-18T10:09:00Z", "mcp__supabase__create_branch", "supabase.mcp.create_branch", BRANCH_REF,
      `create_branch(project_id: "${REF}", name: "import-test")\n→ { "id": "…", "name": "import-test", "project_ref": "${BRANCH_REF}", "status": "CREATING_PROJECT" }`),
  ]),
  exact(R.customers, S.alice, "name_in_container", "The session created public.customers in project acme-dev, and no other session created a table with that name there.", [
    ev(S.alice, 3, "2026-10-26T14:09:00Z", "mcp__supabase__apply_migration", "supabase.mcp.apply_migration.create_table", "public.customers",
      `apply_migration(project_id: "${REF}", name: "create_customers")\ncreate table public.customers (\n  id uuid primary key default gen_random_uuid(),\n  email text not null,\n  name text,\n  imported_at timestamptz default now()\n);`),
  ]),
  exact(R.customerFiles, S.alice, "name_in_container", "The session inserted bucket customer-files into storage.buckets in project acme-dev.", [
    ev(S.alice, 5, "2026-10-26T14:14:00Z", "mcp__supabase__execute_sql", "supabase.sql.insert_bucket", "customer-files",
      `execute_sql(project_id: "${REF}")\ninsert into storage.buckets (id, name, public)\nvalues ('customer-files', 'customer-files', true);`),
  ]),
  exact(R.processImport, S.alice, "id_in_result", "The function slug and project ref appear in the deploy command's output.", [
    ev(S.alice, 8, "2026-10-26T14:22:00Z", "Bash", "supabase.cli.functions_deploy", `${REF}/process-import`,
      `$ SUPABASE_ACCESS_TOKEN=[REDACTED] supabase functions deploy process-import --project-ref ${REF}\nDeployed Functions on project ${REF}: process-import`),
  ]),
  exact(R.previewA81f, S.alice, "id_in_result", "The deployment id appears in the output of vercel deploy.", [
    ev(S.alice, 11, "2026-10-26T14:27:00Z", "Bash", "vercel.cli.deploy", "dpl_SampleA81f000000000000001",
      `$ vercel deploy --yes --token [REDACTED]\nInspect: https://vercel.com/acme/acme-web/SampleA81f [2s]\nPreview: https://acme-web-a81f-sample.vercel.app [41s]\n{ "deployment": { "id": "dpl_SampleA81f000000000000001" } }`),
  ]),
  exact(R.waitlist, S.bob, "name_in_container", "The session created public.waitlist in project acme-dev; the table is known only from this evidence.", [
    ev(S.bob, 2, "2026-10-26T14:09:00Z", "mcp__supabase__apply_migration", "supabase.mcp.apply_migration.create_table", "public.waitlist",
      `apply_migration(project_id: "${REF}", name: "create_waitlist")\ncreate table public.waitlist (\n  id bigint generated always as identity primary key,\n  email text unique not null,\n  created_at timestamptz default now()\n);\nalter table public.waitlist enable row level security;`),
  ]),
  exact(R.waitlistPreview, S.bob, "id_in_result", "The deployment id appears in the output of vercel deploy.", [
    ev(S.bob, 6, "2026-10-26T14:20:00Z", "Bash", "vercel.cli.deploy", "dpl_Sample3c71000000000000001",
      `$ vercel deploy --yes\nPreview: https://acme-web-3c71-sample.vercel.app [38s]\n{ "deployment": { "id": "dpl_Sample3c71000000000000001" } }`),
  ]),
  exact(R.productImages, S.carol, "name_in_container", "The session inserted bucket product-images into storage.buckets in project acme-dev.", [
    ev(S.carol, 2, "2026-10-26T14:11:00Z", "mcp__supabase__execute_sql", "supabase.sql.insert_bucket", "product-images",
      `execute_sql(project_id: "${REF}")\ninsert into storage.buckets (id, name, public)\nvalues ('product-images', 'product-images', false);`),
  ]),
  exact(R.stripeWebhook, S.dan, "id_in_result", "The function slug and project ref appear in the deploy command's output.", [
    ev(S.dan, 4, "2026-10-26T14:36:00Z", "Bash", "supabase.cli.functions_deploy", `${REF}/billing-webhook`,
      `$ supabase functions deploy billing-webhook --no-verify-jwt --project-ref ${REF}\nDeployed Functions on project ${REF}: billing-webhook`),
  ]),
  exact(R.stagingProject, S.eve, "name_in_container", "The CLI printed the project name and team, and exactly one project with that name exists in the team.", [
    ev(S.eve, 1, "2026-10-26T13:44:00Z", "Bash", "vercel.cli.project_create", "acme-landing-staging",
      `$ vercel project create acme-landing-staging\nSuccess! Project acme-landing-staging added (acme) [1s]`),
  ]),
  exact(R.stripeEnv, S.eve, "name_in_container", "The session added this variable name to project acme-landing-staging.", [
    ev(S.eve, 3, "2026-10-26T13:49:00Z", "Bash", "vercel.cli.env_add", "NEXT_PUBLIC_STRIPE_SECRET_KEY",
      `$ printf '[REDACTED]' | vercel env add NEXT_PUBLIC_STRIPE_SECRET_KEY preview\nAdded Environment Variable NEXT_PUBLIC_STRIPE_SECRET_KEY to Project acme-landing-staging [312ms]`),
  ]),
  exact(R.stagingDeployOld, S.eve, "id_in_result", "The deployment id appears in the output of vercel deploy.", [
    ev(S.eve, 5, "2026-10-26T13:52:00Z", "Bash", "vercel.cli.deploy", "dpl_SampleE05b000000000000001",
      `$ vercel deploy --yes\nError: Command "npm run build" exited with 1\n{ "deployment": { "id": "dpl_SampleE05b000000000000001", "readyState": "ERROR" } }`),
  ]),
  exact(R.stagingDeploy, S.eve, "id_in_result", "The deployment id appears in the output of vercel deploy.", [
    ev(S.eve, 9, "2026-10-26T14:06:00Z", "Bash", "vercel.cli.deploy", "dpl_SampleE05c000000000000001",
      `$ vercel deploy --yes\nPreview: https://acme-landing-staging-e05c-sample.vercel.app [44s]\n{ "deployment": { "id": "dpl_SampleE05c000000000000001" } }`),
  ]),
  unknown(R.project, "No recorded agent session created this project."),
  unknown(R.avatars, "No recorded agent session created this bucket."),
  unknown(R.webProject, "No recorded agent session created this project."),
  unknown(R.domain, "No recorded agent session added this domain."),
];

// ------------------------------------------------------------------ findings

const SUPABASE_LINT = "https://supabase.com/docs/guides/database/database-linter?lint=0013_rls_disabled_in_public";

const finding = (r: Resource, f: Omit<ExposureFinding, "id" | "resource_id">): ExposureFinding => ({
  id: `${f.rule_id}:${r.id}`,
  resource_id: r.id,
  ...f,
});

const notExposed = (r: Resource, rule_id: string, title: string, signal: ExposureFinding["signal"]): ExposureFinding =>
  finding(r, {
    rule_id,
    state: "not_exposed",
    severity: "info",
    title,
    explanation: "The last successful check found no exposure for this rule.",
    fix: { kind: "steps", text: "Nothing to fix." },
    checked_at: LAST_CHECK,
    signal,
  });

const findings: ExposureFinding[] = [
  finding(R.customers, {
    rule_id: "rls_disabled",
    state: "exposed",
    severity: "critical",
    title: "RLS off: publicly readable",
    explanation:
      "public.customers is in an API-exposed schema with row level security disabled. Anyone with the project's public anon key can read every row.",
    fix: {
      kind: "sql",
      text: "alter table public.customers enable row level security;",
      note: "With RLS on and no policies, the API returns no rows. Add policies for the access your app needs.",
      docs_url: SUPABASE_LINT,
    },
    checked_at: LAST_CHECK,
    signal: "official_advisor",
  }),
  finding(R.customerFiles, {
    rule_id: "public_bucket",
    state: "exposed",
    severity: "high",
    title: "Public bucket",
    explanation: "Anyone with a file's URL can download it from customer-files. No sign-in is needed.",
    fix: {
      kind: "sql",
      text: "update storage.buckets set public = false where id = 'customer-files';",
      note: "Public URLs stop working. Serve files with signed URLs instead.",
    },
    checked_at: LAST_CHECK,
    signal: "api_field",
  }),
  finding(R.stripeEnv, {
    rule_id: "client_exposed_env",
    state: "exposed",
    severity: "high",
    title: "Secret-looking name exposed to the browser",
    explanation:
      "NEXT_PUBLIC_ variables are inlined into client JavaScript, and this name suggests a secret key.",
    caveat: "Based on the variable name only. Muretto never reads env var values.",
    fix: {
      kind: "steps",
      text: "1. Rename it to STRIPE_SECRET_KEY (no NEXT_PUBLIC_ prefix) and read it only on the server.\n2. Rotate the key in Stripe: earlier builds shipped it to browsers.\n3. Redeploy.",
    },
    checked_at: LAST_CHECK,
    signal: "name_pattern",
  }),
  finding(R.previewA81f, {
    rule_id: "public_preview",
    state: "exposed",
    severity: "medium",
    title: "Public preview",
    explanation: "Deployment protection is off for acme-web, so anyone with this preview's URL can open it.",
    caveat: "Protection exceptions and shareable links are not visible to the API.",
    fix: {
      kind: "steps",
      text: "Vercel dashboard → acme-web → Settings → Deployment Protection → turn on Vercel Authentication (Standard Protection).",
    },
    checked_at: LAST_CHECK,
    signal: "api_field",
  }),
  finding(R.waitlistPreview, {
    rule_id: "public_preview",
    state: "unknown",
    severity: "medium",
    title: "Preview protection not checked",
    explanation: "The last check of acme-web's deployment protection failed, so Muretto can't say whether this preview is public.",
    fix: { kind: "steps", text: "Run `muretto doctor` to see why the Vercel check failed." },
    checked_at: "2026-10-26T14:31:00Z",
    signal: "api_field",
  }),
  notExposed(R.waitlist, "rls_disabled", "RLS on", "official_advisor"),
  notExposed(R.productImages, "public_bucket", "Private bucket", "api_field"),
  notExposed(R.avatars, "public_bucket", "Private bucket", "api_field"),
  notExposed(R.stagingDeploy, "public_preview", "Preview protected", "api_field"),
];

// --------------------------------------------------------------------- costs

const cost = (r: Resource, c: Omit<CostRecord, "resource_id" | "as_of"> & Partial<CostRecord>): CostRecord => ({
  resource_id: r.id,
  as_of: LAST_CHECK,
  ...c,
});

const PERIOD = { period_start: "2026-10-01T00:00:00Z", period_end: "2026-10-26T00:00:00Z" };

const costs: CostRecord[] = [
  cost(R.project, { source: "estimated", amount_micros: 10_000_000, currency: "USD", interval: "monthly", basis: "Supabase add-on price: Micro compute" }),
  cost(R.branch, { source: "estimated", amount_micros: 13_440, currency: "USD", interval: "hourly", basis: "Supabase branch compute price: Micro" }),
  cost(R.webProject, { source: "reported", amount_micros: 3_480_000, currency: "USD", interval: "period", ...PERIOD, basis: "Vercel billing charges, grouped by project" }),
  cost(R.stagingProject, { source: "reported", amount_micros: 120_000, currency: "USD", interval: "period", ...PERIOD, basis: "Vercel billing charges, grouped by project" }),
  ...[R.customers, R.waitlist].map((r) => cost(r, { source: "unknown", basis: "Supabase reports no cost per table" })),
  ...[R.customerFiles, R.productImages, R.avatars].map((r) => cost(r, { source: "unknown", basis: "Supabase reports no cost per bucket" })),
  ...[R.processImport, R.stripeWebhook].map((r) => cost(r, { source: "unknown", basis: "Supabase reports no cost per function" })),
  ...[R.previewA81f, R.waitlistPreview, R.stagingDeploy].map((r) =>
    cost(r, { source: "unknown", basis: "Vercel reports cost per project, not per deployment" }),
  ),
];

// ------------------------------------------------------------------- reviews

const review = (r: Resource, owner: Developer | null, due_at: string, extra: Partial<ReviewItem> = {}): ReviewItem => ({
  resource_id: r.id,
  ...(owner ? { owner_developer_id: owner.id } : {}),
  due_at,
  state: "pending",
  ...extra,
});

const reviews: ReviewItem[] = [
  review(R.branch, alice, "2026-10-25T10:09:00Z"),
  review(R.avatars, null, "2026-10-20T16:40:00Z"),
  review(R.customers, alice, "2026-11-02T14:09:00Z"),
  review(R.customerFiles, alice, "2026-11-02T14:14:00Z"),
  review(R.previewA81f, alice, "2026-11-02T14:27:00Z"),
  review(R.waitlistPreview, bob, "2026-11-02T14:20:00Z"),
  review(R.productImages, carol, "2026-11-02T14:11:00Z"),
  review(R.stripeWebhook, dan, "2026-11-02T14:36:00Z"),
  review(R.stagingDeploy, eve, "2026-11-02T14:06:00Z"),
  review(R.stagingProject, eve, "2026-11-02T13:44:00Z", {
    state: "keep",
    decided_by: eve.id,
    decided_at: "2026-10-26T14:13:00Z",
    note: "staging stays until launch",
  }),
];

// -------------------------------------------------------------------- checks

const check = (c: Omit<ProviderCheck, "status" | "attempted_at" | "last_success_at"> & Partial<ProviderCheck>): ProviderCheck => ({
  status: "ok",
  attempted_at: LAST_CHECK,
  last_success_at: LAST_CHECK,
  ...c,
});

const checks: ProviderCheck[] = [
  check({ provider: "supabase", account_id: SB_ACCOUNT.account_id, kind: "supabase.project", items: 1 }),
  check({ provider: "supabase", account_id: SB_ACCOUNT.account_id, kind: "supabase.branch", scope: REF, items: 1 }),
  check({ provider: "supabase", account_id: SB_ACCOUNT.account_id, kind: "supabase.bucket", scope: REF, items: 3, rule_id: "public_bucket" }),
  check({ provider: "supabase", account_id: SB_ACCOUNT.account_id, kind: "supabase.function", scope: REF, items: 2 }),
  check({ provider: "supabase", account_id: SB_ACCOUNT.account_id, kind: "supabase.table", scope: REF, items: 1, rule_id: "rls_disabled" }),
  check({ provider: "vercel", account_id: VC_ACCOUNT.account_id, kind: "vercel.project", items: 2 }),
  check({ provider: "vercel", account_id: VC_ACCOUNT.account_id, kind: "vercel.deployment", items: 3 }),
  check({ provider: "vercel", account_id: VC_ACCOUNT.account_id, kind: "vercel.domain", items: 1 }),
  check({ provider: "vercel", account_id: VC_ACCOUNT.account_id, kind: "vercel.env_var", scope: PRJ_STAGING, items: 1, rule_id: "client_exposed_env" }),
  check({ provider: "vercel", account_id: VC_ACCOUNT.account_id, kind: "vercel.deployment", scope: PRJ_STAGING, rule_id: "public_preview" }),
  check({
    provider: "vercel",
    account_id: VC_ACCOUNT.account_id,
    kind: "vercel.deployment",
    scope: PRJ_WEB,
    rule_id: "public_preview",
    status: "error",
    error_code: "rate_limit",
    error: "Vercel API rate limit reached; Muretto retries on the next command.",
    last_success_at: "2026-10-26T14:31:00Z",
  }),
];

const warnings: Warning[] = [
  {
    code: "tables_partial",
    message:
      "Supabase tables are listed only when an agent session created them or the security advisor flags them.",
    fix: "Full table listing needs the opt-in Database (Read) permission. See `muretto providers --coverage`.",
  },
];

// ------------------------------------------------------------- the Answer(s)

function sessionViews(sessions: Session[], atts: Attribution[], fs: ExposureFinding[], rs: ReviewItem[]): SessionView[] {
  const now = Date.parse(SAMPLE_NOW);
  return sessions.map((s) => {
    const ids = atts.filter((a) => a.label === "exact" && a.session_id === s.id).map((a) => a.resource_id);
    return {
      session: s,
      developer_name: developers.find((d) => d.id === s.developer_id)?.display_name ?? "unknown",
      resource_ids: ids,
      exposed_count: fs.filter((f) => f.state === "exposed" && ids.includes(f.resource_id)).length,
      review_due_count: rs.filter((r) => r.state === "pending" && ids.includes(r.resource_id) && Date.parse(r.due_at) <= now).length,
    };
  });
}

function summarize(views: SessionView[], resources: Resource[], atts: Attribution[], fs: ExposureFinding[], rs: ReviewItem[], cs: ProviderCheck[]): Summary {
  const now = Date.parse(SAMPLE_NOW);
  const exactIds = new Set(atts.filter((a) => a.label === "exact").map((a) => a.resource_id));
  const ok = cs.filter((c) => c.last_success_at).map((c) => c.last_success_at!);
  return {
    sessions: views.length,
    agent_created: exactIds.size,
    still_existing: resources.filter((r) => exactIds.has(r.id) && r.existence === "confirmed").length,
    exposed: fs.filter((f) => f.state === "exposed").length,
    needs_review: rs.filter((r) => r.state === "pending" && Date.parse(r.due_at) <= now).length,
    unknown_creator: resources.filter((r) => !exactIds.has(r.id)).length,
    checks_ok: cs.filter((c) => c.status === "ok").length,
    checks_failed: cs.filter((c) => c.status === "error").length,
    oldest_check_at: ok.sort()[0],
  };
}

function buildAnswer(viewer: Developer, sessions: Session[]): Answer {
  const sessionIds = new Set(sessions.map((s) => s.id));
  // A developer's own data: what their sessions created, plus everything their
  // connected accounts show with no creator (nothing is hidden).
  const atts = attributions.filter((a) => a.label === "unknown" || sessionIds.has(a.session_id ?? ""));
  const ids = new Set(atts.map((a) => a.resource_id));
  const resources = Object.values(R).filter((r) => ids.has(r.id));
  const fs = findings.filter((f) => ids.has(f.resource_id));
  const rs = reviews.filter((r) => ids.has(r.resource_id));
  const views = sessionViews(sessions, atts, fs, rs);
  return {
    schema_version: 1,
    generated_at: LAST_CHECK,
    developer: viewer,
    summary: summarize(views, resources, atts, fs, rs, checks),
    sessions: views,
    resources,
    attributions: atts,
    findings: fs,
    costs: costs.filter((c) => ids.has(c.resource_id)),
    reviews: rs,
    checks,
    warnings,
  };
}

/** Team scope: the union of every member's data. */
export const sampleTeamAnswer: Answer = buildAnswer(alice, Object.values(S));

/** Me scope: alice's own sessions. */
export const sampleMeAnswer: Answer = buildAnswer(
  alice,
  Object.values(S).filter((s) => s.developer_id === alice.id),
);

export const sampleMe: Me = { login: "alice-sample", display_name: "alice", orgs: [SAMPLE_ORG] };

/** The session the landing page renders as "git log for infrastructure". */
export const SAMPLE_SHOWCASE_SESSION = S.alice.id;
