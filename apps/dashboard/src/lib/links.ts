// A resource's own URL: the live thing (a preview, a domain) or its page in
// the provider's console. Only https URLs are ever linked.

const CONSOLES = [
  "supabase.com",
  "vercel.com",
  "dash.cloudflare.com",
  "console.neon.tech",
  "dashboard.stripe.com",
  "dashboard.render.com",
  "app.planetscale.com",
  "github.com",
];

export function safeUrl(u?: string): string | undefined {
  return u && /^https:\/\/[^\s]+$/i.test(u) ? u : undefined;
}

/** True when the URL is the provider's console, not the thing itself. */
export function isConsole(u: string): boolean {
  try {
    const host = new URL(u).hostname;
    return CONSOLES.some((c) => host === c || host.endsWith(`.${c}`));
  } catch {
    return false;
  }
}

/** "demo-web-a81f.vercel.app", without the scheme or a trailing slash. */
export function linkText(u: string): string {
  try {
    const x = new URL(u);
    return (x.host + x.pathname).replace(/\/$/, "");
  } catch {
    return u;
  }
}
