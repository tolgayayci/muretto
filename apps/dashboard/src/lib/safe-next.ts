/**
 * Where to go after signing in. Only same-site relative paths are allowed, so a
 * crafted link can't bounce the user to another site (open redirect).
 */
export function safeNext(v: string | null | undefined): string {
  if (!v || v.length > 512) return "/";
  if (!v.startsWith("/") || v.startsWith("//") || v.startsWith("/\\")) return "/";
  if (/[\\\r\n\t]/.test(v)) return "/";
  if (v.startsWith("/auth/") || v.startsWith("/login")) return "/";
  return v;
}
