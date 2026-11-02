// Redirects and same-origin checks that hold behind a proxy. On a host like
// Railway, a route handler's request.nextUrl carries the server's internal
// address (https://localhost:3000), so redirects use a relative Location and
// the origin check compares against the Host the browser asked for.
import { NextResponse, type NextRequest } from "next/server";

/** A redirect to a path on this site; the browser resolves it against the page it is on. */
export function redirectHere(path: string, status: 303 | 307 = 307): NextResponse {
  if (!path.startsWith("/") || path.startsWith("//")) throw new Error("redirectHere takes a site path");
  return new NextResponse(null, { status, headers: { Location: path } });
}

/** False when a request carries an Origin from another site. A missing Origin passes. */
export function sameOrigin(request: Pick<NextRequest, "headers">): boolean {
  const origin = request.headers.get("origin");
  if (!origin) return true;
  const host = request.headers.get("host");
  try {
    return !!host && new URL(origin).host === host;
  } catch {
    return false;
  }
}
