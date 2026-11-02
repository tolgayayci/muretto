// Environment, read once. NEXT_PUBLIC_* values are inlined at build time.
export const SUPABASE_URL = process.env.NEXT_PUBLIC_SUPABASE_URL ?? "";
export const SUPABASE_KEY = process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY ?? process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY ?? "";

/** The sync API, called only from the dashboard's server code. */
export const API_URL = (process.env.MURETTO_API_URL ?? "http://127.0.0.1:8080").replace(/\/+$/, "");

/**
 * Local development without a Supabase project. Never on in production builds
 * (`next build` / `next start` run with NODE_ENV=production).
 */
export const DEV_AUTH = process.env.MURETTO_DEV_AUTH === "1" && process.env.NODE_ENV !== "production";

export const supabaseConfigured = SUPABASE_URL !== "" && SUPABASE_KEY !== "";

/**
 * The Supabase session cookies hold the access and refresh tokens: Secure in
 * production builds, so a plain-http visit never carries them. (Browsers treat
 * http://localhost as secure, so `next start` locally still works.)
 */
export const SUPABASE_COOKIES = { secure: process.env.NODE_ENV === "production" };

/** Cookies used by the local dev login (dev builds only). */
export const DEV_TOKEN_COOKIE = "mrt_dev";
export const DEV_SAMPLE_COOKIE = "mrt_sample";

/** Paths that don't need a signed-in user. */
export function isPublicPath(path: string): boolean {
  return path === "/login" || path.startsWith("/auth/") || path.startsWith("/dev/");
}
