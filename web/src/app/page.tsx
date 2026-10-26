import Link from "next/link";
import { ProviderRequestForm } from "@/components/landing/provider-request";
import { SessionTerminal } from "@/components/landing/terminal";
import { SamplePreview } from "@/components/dashboard/sample";
import { CopyButton, Icon, SampleBadge, Wordmark } from "@/components/ui";

const REPO = "https://github.com/tolgayayci/muretto";
const ADAPTER_DOC = `${REPO}/blob/main/docs/WRITING_AN_ADAPTER.md`;
const SECURITY_DOC = `${REPO}/blob/main/SECURITY.md`;

function Install({ className = "" }: { className?: string }) {
  return (
    <div
      className={`inline-flex h-12 items-center gap-4 rounded-xl border border-line-strong bg-panel py-1 pr-1.5 pl-4 font-mono text-[15px] shadow-panel ${className}`}
    >
      <span>
        <span className="text-faint select-none">$ </span>npx muretto
      </span>
      <CopyButton text="npx muretto" variant="solid" />
    </div>
  );
}

function SectionHead({ eyebrow, title, children }: { eyebrow: string; title: React.ReactNode; children?: React.ReactNode }) {
  return (
    <div className="max-w-2xl">
      <p className="font-mono text-xs tracking-[0.12em] text-accent uppercase">{eyebrow}</p>
      <h2 className="mt-3 text-[28px] leading-[1.15] font-semibold tracking-[-0.02em] sm:text-[34px]">{title}</h2>
      {children ? <p className="mt-4 text-[16px] leading-relaxed text-muted">{children}</p> : null}
    </div>
  );
}

const STEPS = [
  {
    n: "01",
    title: "The story",
    body: "Claude Code hooks record what each agent session ran. The hook writes one small, redacted file and exits: no network, and it never blocks the agent.",
  },
  {
    n: "02",
    title: "The truth",
    body: "Read-only API calls list what actually exists in your connected Supabase and Vercel accounts, including what no agent claims.",
  },
  {
    n: "03",
    title: "The join",
    body: "Every resource gets a label: exact, with stored evidence you can inspect, or creator unknown. Never a guess.",
  },
];

const RULES = [
  ["Tables with RLS off", "readable with the public key", "Supabase"],
  ["Public storage buckets", "files downloadable by URL", "Supabase"],
  ["Public preview deployments", "no deployment protection", "Vercel"],
  ["Secret-looking client env vars", "NEXT_PUBLIC_*SECRET*", "Vercel"],
];

const COVERAGE = [
  { provider: "Supabase", kinds: ["projects", "branches", "tables", "storage buckets", "edge functions"] },
  { provider: "Vercel", kinds: ["projects", "deployments and preview URLs", "domains", "env var names"] },
];

const SECURITY = [
  {
    icon: "shield" as const,
    title: "Read-only, enforced in code",
    body: "Every provider request goes through one client with an allowlist of read endpoints. Anything else fails before it reaches the network, and a test proves it.",
  },
  {
    icon: "code" as const,
    title: "Open source",
    body: "The CLI and adapters are Apache-2.0. Read exactly what runs on your machine and what it sends.",
  },
  {
    icon: "laptop" as const,
    title: "Local-first",
    body: "Everything works without an account. Secrets are redacted when recorded and again on output. Tokens live in your OS keychain.",
  },
  {
    icon: "toggle" as const,
    title: "Opt-in sync",
    body: "Sync is off until you turn it on, and the CLI shows exactly which fields will be sent first. Never tokens, transcripts or full prompts.",
  },
];

export default function Landing() {
  return (
    <div className="overflow-x-clip">
      <header className="sticky top-0 z-30 border-b border-line/70 bg-bg/80 backdrop-blur">
        <nav aria-label="Main" className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-5">
          <Link href="/" className="rounded-md" aria-label="Muretto home">
            <Wordmark />
          </Link>
          <div className="hidden items-center gap-5 text-[13px] text-muted md:flex">
            <a href="#how" className="hover:text-fg">How it works</a>
            <a href="#adapters" className="hover:text-fg">Adapters</a>
            <a href="#security" className="hover:text-fg">Security</a>
            <a href="#team" className="hover:text-fg">Team</a>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <a
              href={REPO}
              className="inline-flex size-9 items-center justify-center rounded-lg text-muted hover:bg-hover hover:text-fg"
              aria-label="Muretto on GitHub"
            >
              <Icon name="github" className="size-[18px]" />
            </a>
            <Link
              href="/app"
              className="inline-flex h-9 items-center rounded-lg border border-line-strong px-3.5 text-[13px] font-medium hover:bg-hover"
            >
              Dashboard
            </Link>
          </div>
        </nav>
      </header>

      <main>
        {/* ------------------------------------------------------------ hero */}
        <section className="relative">
          <div className="grid-bg pointer-events-none absolute inset-0 [mask-image:radial-gradient(70%_60%_at_30%_30%,black,transparent)]" aria-hidden="true" />
          <div className="relative mx-auto grid max-w-6xl gap-12 px-5 pt-16 pb-20 sm:pt-24 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.08fr)] lg:items-center lg:gap-14">
            <div>
              <p className="inline-flex items-center gap-2 rounded-full border border-line bg-panel px-3 py-1 font-mono text-[11px] tracking-wide text-muted">
                <span className="size-1.5 rounded-full bg-accent" aria-hidden="true" />
                git log for infrastructure · for Claude Code
              </p>
              <h1 className="mt-6 text-[40px] leading-[1.04] font-semibold tracking-[-0.035em] text-balance sm:text-[56px]">
                See everything your AI agents create{" "}
                <span className="font-display font-normal tracking-[-0.01em] text-accent italic">outside the repo.</span>
              </h1>
              <p className="mt-6 max-w-xl text-[17px] leading-relaxed text-muted text-pretty">
                Muretto shows which agent session created every database, bucket and deployment, what&apos;s exposed, and how
                to fix it. Read-only. Never touches your infrastructure.
              </p>
              <div className="mt-8 flex flex-wrap items-center gap-4">
                <Install />
                <Link href="/app/sample" className="group inline-flex items-center gap-1.5 text-sm text-muted hover:text-fg">
                  See the dashboard
                  <Icon name="chevron" className="size-3.5 transition-transform group-hover:translate-x-0.5" />
                </Link>
              </div>
              <p className="mt-4 text-xs text-faint">macOS and Linux · works without an account</p>
            </div>
            <SessionTerminal />
          </div>
        </section>

        {/* ------------------------------------------------------------ how */}
        <section id="how" className="scroll-mt-16 border-t border-line">
          <div className="mx-auto max-w-6xl px-5 py-20">
            <SectionHead eyebrow="How it works" title="Two sources, one join.">
              Git tracks your code. Nothing tracked what your agents created outside it, until now.
            </SectionHead>
            <ol className="mt-12 grid gap-px overflow-hidden rounded-2xl border border-line bg-line md:grid-cols-3">
              {STEPS.map((s) => (
                <li key={s.n} className="bg-panel p-6">
                  <p className="font-mono text-xs text-faint">{s.n}</p>
                  <h3 className="mt-3 text-[17px] font-semibold tracking-[-0.01em]">{s.title}</h3>
                  <p className="mt-2 text-sm leading-relaxed text-muted">{s.body}</p>
                </li>
              ))}
            </ol>
            <div className="mt-6 grid gap-6 rounded-2xl border border-line bg-panel p-6 md:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
              <div>
                <h3 className="text-[17px] font-semibold tracking-[-0.01em]">What it flags</h3>
                <p className="mt-2 text-sm leading-relaxed text-muted">
                  Each check is exposed, not exposed or unknown, and shows when it last ran. Every finding comes with the exact
                  command or SQL to fix it. Muretto prints it; you run it.
                </p>
              </div>
              <ul className="divide-y divide-line text-sm">
                {RULES.map(([title, detail, provider]) => (
                  <li key={title} className="flex items-center gap-3 py-2.5">
                    <Icon name="alert" className="size-4 shrink-0 text-danger" />
                    <span className="font-medium">{title}</span>
                    <span className="hidden truncate text-muted sm:inline">{detail}</span>
                    <span className="ml-auto font-mono text-xs text-faint">{provider}</span>
                  </li>
                ))}
              </ul>
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ adapters */}
        <section id="adapters" className="scroll-mt-16 border-t border-line">
          <div className="mx-auto max-w-6xl px-5 py-20">
            <SectionHead eyebrow="Adapters" title="Starting with Supabase + Vercel.">
              Starting with Supabase + Vercel, the default stack for agent-built apps. Every provider is a declarative,
              read-only adapter: we maintain the official ones, anyone can write one.
            </SectionHead>
            <div className="mt-12 grid gap-4 md:grid-cols-2">
              {COVERAGE.map((c) => (
                <div key={c.provider} className="rounded-2xl border border-line bg-panel p-6">
                  <div className="flex items-center justify-between">
                    <h3 className="text-[17px] font-semibold">{c.provider}</h3>
                    <span className="rounded-full border border-accent-line px-2 py-0.5 font-mono text-[11px] text-accent">official</span>
                  </div>
                  <ul className="mt-4 flex flex-wrap gap-2">
                    {c.kinds.map((k) => (
                      <li key={k} className="rounded-md border border-line bg-sunken px-2 py-1 font-mono text-xs text-muted">
                        {k}
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
            <div className="mt-4 grid gap-4 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
              <div className="rounded-2xl border border-line bg-panel p-6">
                <h3 className="text-[17px] font-semibold">Request a provider</h3>
                <p className="mt-1 mb-5 text-sm text-muted">Tell us what your agents create next.</p>
                <ProviderRequestForm />
              </div>
              <div className="flex flex-col rounded-2xl border border-line bg-panel p-6">
                <h3 className="text-[17px] font-semibold">Write an adapter</h3>
                <p className="mt-1 text-sm leading-relaxed text-muted">
                  An adapter is one YAML file: read-only list endpoints, field mapping, the signatures of agent commands that
                  create things, exposure rules and fix text. No Go changes needed.
                </p>
                <a
                  href={ADAPTER_DOC}
                  className="mt-auto inline-flex h-10 items-center gap-2 self-start rounded-lg border border-line-strong px-4 text-sm font-medium hover:bg-hover"
                >
                  Read WRITING_AN_ADAPTER.md <Icon name="external" className="size-3.5 text-muted" />
                </a>
              </div>
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ security */}
        <section id="security" className="scroll-mt-16 border-t border-line">
          <div className="mx-auto max-w-6xl px-5 py-20">
            <SectionHead eyebrow="Security" title="Built to be trusted with your accounts.">
              Muretto sees your infrastructure and never touches it. It shows, reminds and prints fixes. It never creates, changes,
              pauses or deletes anything.
            </SectionHead>
            <ul className="mt-12 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
              {SECURITY.map((s) => (
                <li key={s.title} className="rounded-2xl border border-line bg-panel p-6">
                  <Icon name={s.icon} className="size-5 text-accent" />
                  <h3 className="mt-4 text-[15px] font-semibold">{s.title}</h3>
                  <p className="mt-2 text-sm leading-relaxed text-muted">{s.body}</p>
                </li>
              ))}
            </ul>
            <a href={SECURITY_DOC} className="mt-6 inline-flex items-center gap-1.5 text-sm text-muted hover:text-fg">
              Read the security model <Icon name="external" className="size-3.5" />
            </a>
          </div>
        </section>

        {/* ------------------------------------------------------------ team */}
        <section id="team" className="scroll-mt-16 border-t border-line">
          <div className="mx-auto max-w-6xl px-5 pt-20">
            <div className="flex flex-wrap items-end justify-between gap-6">
              <SectionHead eyebrow="Team" title="Get your team on Muretto.">
                Sign the CLI in, turn sync on, and everyone sees agent-created resources appear live: who made them, in which
                session, and what&apos;s exposed.
              </SectionHead>
              <div className="flex flex-col items-start gap-2 text-sm text-muted">
                <code className="font-mono text-fg">muretto login</code>
                <code className="font-mono text-fg">muretto sync on</code>
              </div>
            </div>
          </div>
          <div className="mx-auto mt-12 max-w-[1240px] px-3 sm:px-5">
            <figure className="relative overflow-hidden rounded-t-2xl border border-b-0 border-line-strong bg-bg shadow-panel">
              <div className="flex h-9 items-center gap-2 border-b border-line bg-panel px-4">
                <span className="size-2.5 rounded-full bg-line-strong" />
                <span className="size-2.5 rounded-full bg-line-strong" />
                <span className="size-2.5 rounded-full bg-line-strong" />
                <SampleBadge className="ml-auto scale-90" />
              </div>
              <div className="pointer-events-none relative h-[640px] overflow-hidden sm:h-[720px]">
                <SamplePreview />
                <div className="absolute inset-x-0 bottom-0 h-40 bg-gradient-to-t from-bg to-transparent" aria-hidden="true" />
              </div>
              <figcaption className="sr-only">
                The Muretto team dashboard, rendered live from sample data with fake names.
              </figcaption>
            </figure>
          </div>
          <div className="border-t border-line">
            <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-4 px-5 py-6 text-sm text-muted">
              <span>The preview above is the real dashboard, running on sample data.</span>
              <Link href="/app/sample" className="inline-flex items-center gap-1.5 text-fg hover:underline">
                Open it <Icon name="chevron" className="size-3.5" />
              </Link>
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ install */}
        <section className="border-t border-line">
          <div className="mx-auto flex max-w-6xl flex-col items-center px-5 py-24 text-center">
            <h2 className="text-[32px] leading-tight font-semibold tracking-[-0.025em] sm:text-[40px]">
              Know what your agents <span className="font-display font-normal text-accent italic">left behind.</span>
            </h2>
            <p className="mt-4 max-w-lg text-muted">Run it in any project where you use Claude Code. No account needed.</p>
            <Install className="mt-8" />
          </div>
        </section>
      </main>

      <footer className="border-t border-line">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-3 px-5 py-8 text-xs text-faint">
          <Wordmark className="text-fg" />
          <span>CLI and adapters: Apache-2.0</span>
          <a href={REPO} className="hover:text-fg">GitHub</a>
          <a href={SECURITY_DOC} className="hover:text-fg">Security</a>
          <a href={ADAPTER_DOC} className="hover:text-fg">Write an adapter</a>
          <span className="ml-auto">Read-only. Never touches your infrastructure.</span>
        </div>
      </footer>
    </div>
  );
}
