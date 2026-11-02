import type { Metadata } from "next";
import Link from "next/link";
import { Badge, Icon, Mark } from "@/components/ds";
import { ProjectBadge, ProviderBadge } from "@/components/brand";
import { GitHubButton, GitHubMark } from "@/components/github-button";
import { DEV_AUTH, supabaseConfigured } from "@/lib/config";
import { safeNext } from "@/lib/safe-next";
import { SCENARIOS } from "@/lib/sample";

export const metadata: Metadata = { title: "Sign in" };

const ERRORS: Record<string, string> = {
  denied: "GitHub sign-in was cancelled.",
  missing_code: "That sign-in link didn't work. Try again.",
  exchange_failed: "That sign-in link didn't work. Try again.",
  expired: "Your session ended. Sign in again.",
  dev_api: "The local API didn't answer. Start murettod with MURETTO_DEV_LOGIN=1.",
};

export default async function LoginPage({ searchParams }: { searchParams: Promise<{ next?: string; error?: string }> }) {
  const sp = await searchParams;
  const next = safeNext(sp.next);
  const error = sp.error ? ERRORS[sp.error] ?? "Sign-in didn't work. Try again." : null;

  return (
    <div style={{ minHeight: "100vh", display: "flex", flexWrap: "wrap" }}>
      <section aria-label="Sign in" style={{ flex: "1 1 600px", minWidth: 0, boxSizing: "border-box", padding: "32px 64px", display: "flex", flexDirection: "column" }}>
        <header style={{ height: 40, display: "flex", alignItems: "center" }}>
          <Mark size={26} wordmark />
        </header>

        <div style={{ flexGrow: 1, display: "flex", flexDirection: "column", justifyContent: "center", gap: 36, maxWidth: 600, padding: "48px 0" }}>
          <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
            <p className="m-eyebrow">Dashboard</p>
            <h1 style={{ margin: 0, fontSize: 48, lineHeight: 1.05, fontWeight: 600, letterSpacing: "-0.04em" }}>
              <span style={{ display: "block" }}>Your agent said “Done.”</span>
              <span style={{ display: "block", color: "var(--ink-faint)" }}>See what it actually built.</span>
            </h1>
            <p style={{ margin: 0, maxWidth: 500, fontSize: 17, lineHeight: "26px", color: "var(--ink-muted)" }}>
              Every agent session, what it created in Supabase and Vercel, and what&apos;s open to the internet.
            </p>
          </div>

          <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
            {error ? (
              <p role="alert" style={{ margin: 0, padding: "12px 14px", border: "1px solid var(--line-strong)", borderRadius: "var(--radius-md)", fontSize: 14, color: "var(--ink-muted)" }}>
                {error}
              </p>
            ) : null}
            {supabaseConfigured ? <GitHubButton next={next} /> : null}
            {DEV_AUTH ? (
              <div style={{ display: "flex", flexDirection: "column", gap: 10, padding: 14, border: "1px dashed var(--line-strong)", borderRadius: "var(--radius-lg)" }}>
                <span style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)" }}>development only</span>
                <form action="/dev/login" method="post" className="m-full">
                  <input type="hidden" name="next" value={next} />
                  <button type="submit" className="mu-btn mu-btn-secondary">
                    Sign in to the local API as alice
                  </button>
                </form>
                <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                  {SCENARIOS.map((s) => (
                    <Link key={s} href={`/dev/sample?scenario=${s}&next=${encodeURIComponent(next)}`} prefetch={false} className="m-chip" style={{ height: 30, fontSize: 12.5 }}>
                      sample: {s}
                    </Link>
                  ))}
                </div>
              </div>
            ) : null}
            {!supabaseConfigured && !DEV_AUTH ? (
              <p role="alert" style={{ margin: 0, fontSize: 14, color: "var(--ink-muted)" }}>
                Sign-in isn&apos;t set up on this server yet. Set NEXT_PUBLIC_SUPABASE_URL and NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY.
              </p>
            ) : null}
          </div>

          <div style={{ display: "flex", flexDirection: "column", gap: 10, paddingTop: 24, borderTop: "1px solid var(--line)" }}>
            <span style={{ display: "flex", alignItems: "center", gap: 10, fontSize: 14, color: "var(--ink-faint)" }}>
              <Icon name="lock" size={16} /> GitHub only tells us who you are. No access to your code.
            </span>
            <span style={{ display: "flex", alignItems: "center", gap: 10, fontSize: 14, color: "var(--ink-faint)" }}>
              <Icon name="search" size={16} /> Muretto reads your providers. It changes nothing.
            </span>
          </div>
        </div>

        <footer style={{ display: "flex", flexWrap: "wrap", justifyContent: "space-between", gap: 10, font: "12.5px var(--font-mono)", color: "var(--ink-faint)" }}>
          <span>Read-only · Open source</span>
          <span>Apache-2.0</span>
        </footer>
      </section>

      <section aria-label="What Muretto shows" style={{ flex: "1 1 520px", minWidth: 0, boxSizing: "border-box", padding: "32px 48px", background: "var(--surface-sunken)", borderLeft: "1px solid var(--line)", display: "flex", flexDirection: "column" }}>
        <div style={{ height: 40, display: "flex", alignItems: "center", justifyContent: "flex-end" }}>
          <a className="mu-btn mu-btn-secondary mu-btn-sm" href="https://github.com/tolgayayci/muretto" target="_blank" rel="noopener noreferrer">
            <GitHubMark size={16} />
            Star on GitHub
          </a>
        </div>
        <div aria-hidden style={{ flexGrow: 1, display: "flex", flexDirection: "column", justifyContent: "center", padding: "32px 0" }}>
          <div className="mu-agent" style={{ maxWidth: "none", borderRadius: "var(--radius-2xl)", boxShadow: "none" }}>
            <div className="mu-agent-head">
              <span className="mu-agent-avatar"><Icon name="sparkle" size={13} strokeWidth={2.2} /></span>
              <span>Your coding agent</span>
              <span className="mu-agent-time">14:31</span>
            </div>
            <p className="mu-agent-text">Done! Customer import is ready.</p>
            <p className="mu-agent-status">✓ Task complete</p>
          </div>
          <div style={{ display: "grid", gridTemplateColumns: "1fr auto 1fr", alignItems: "center", height: 72 }}>
            <span />
            <span style={{ height: "100%", display: "flex", flexDirection: "column", alignItems: "center" }}>
              <span style={{ flexGrow: 1, width: 1, background: "var(--line-strong)" }} />
              <span style={{ width: 30, height: 30, boxSizing: "border-box", borderRadius: 999, border: "1px solid var(--line-strong)", background: "var(--surface)", display: "inline-flex", alignItems: "center", justifyContent: "center", color: "var(--ink-muted)" }}>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 5v14M6 13l6 6 6-6" /></svg>
              </span>
              <span style={{ flexGrow: 1, width: 1, background: "var(--line-strong)" }} />
            </span>
            <span style={{ paddingLeft: 14, font: "12.5px var(--font-mono)", color: "var(--ink-faint)" }}>what Muretto found</span>
          </div>
          <div style={{ border: "1px solid var(--line-strong)", borderRadius: "var(--radius-2xl)", background: "var(--surface)", boxShadow: "var(--shadow-float)", overflow: "hidden" }}>
            <div style={{ height: 44, padding: "0 18px", borderBottom: "1px solid var(--line)", display: "flex", alignItems: "center", justifyContent: "flex-end", gap: 8, font: "12px var(--font-mono)", color: "var(--ink-faint)" }}>
              <span className="m-dot" />
              synced 5s ago
            </div>
            <div style={{ padding: "26px 26px 28px", display: "flex", flexDirection: "column", gap: 20 }}>
              <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
                <span style={{ font: "500 12px/16px var(--font-mono)", color: "var(--accent)" }}>Overview · Personal</span>
                <span style={{ fontSize: 26, lineHeight: "30px", fontWeight: 600, letterSpacing: "-0.03em" }}>
                  <span style={{ display: "block" }}>3 things are open to anyone.</span>
                  <span style={{ display: "block", color: "var(--ink-faint)" }}>All three came from one agent session.</span>
                </span>
              </div>
              <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                {[
                  ["customers", "supabase", "acme-dev", "Anyone can read"],
                  ["customer-files", "supabase", "acme-dev", "Anyone can download"],
                  ["preview-a81f", "vercel", "acme-web", "Anyone can open"],
                ].map(([name, provider, project, badge]) => (
                  <div key={name} style={{ display: "flex", alignItems: "center", gap: 10, padding: "10px 12px", border: "1px solid var(--line)", borderRadius: "var(--radius-md)", background: "var(--surface-sunken)" }}>
                    <span style={{ font: "500 13.5px var(--font-mono)", flexGrow: 1 }}>{name}</span>
                    <ProviderBadge provider={provider} />
                    <ProjectBadge name={project} />
                    <Badge tone="exposed">{badge}</Badge>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      </section>
    </div>
  );
}
