// Runs before every page: sets a per-request nonce CSP, refreshes the Supabase
// session cookies, and sends signed-out visitors to /login.
import { NextResponse, type NextRequest } from "next/server";
import { createServerClient } from "@supabase/ssr";
import { DEV_AUTH, DEV_SAMPLE_COOKIE, DEV_TOKEN_COOKIE, SUPABASE_COOKIES, SUPABASE_KEY, SUPABASE_URL, isPublicPath, supabaseConfigured } from "@/lib/config";

function contentSecurityPolicy(nonce: string): string {
  const dev = process.env.NODE_ENV === "development";
  const supabase = SUPABASE_URL ? ` ${new URL(SUPABASE_URL).origin}` : "";
  return [
    "default-src 'self'",
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'${dev ? " 'unsafe-eval'" : ""}`,
    `style-src 'self' 'nonce-${nonce}'`,
    // React style attributes (avatar colors, sizes). <style> elements still need the nonce.
    "style-src-attr 'unsafe-inline'",
    "img-src 'self' data: blob: https://avatars.githubusercontent.com",
    "font-src 'self'",
    `connect-src 'self'${supabase}`,
    "object-src 'none'",
    "base-uri 'self'",
    `form-action 'self'${supabase} https://github.com`,
    "frame-ancestors 'none'",
    ...(dev ? [] : ["upgrade-insecure-requests"]),
  ].join("; ");
}

export async function proxy(request: NextRequest) {
  const nonce = Buffer.from(crypto.randomUUID()).toString("base64");
  const csp = contentSecurityPolicy(nonce);
  const headers = new Headers(request.headers);
  headers.set("x-nonce", nonce);
  headers.set("Content-Security-Policy", csp);

  let response = NextResponse.next({ request: { headers } });
  const path = request.nextUrl.pathname;
  let signedIn = false;

  if (DEV_AUTH && (request.cookies.has(DEV_TOKEN_COOKIE) || request.cookies.has(DEV_SAMPLE_COOKIE))) {
    signedIn = true;
  } else if (supabaseConfigured) {
    const supabase = createServerClient(SUPABASE_URL, SUPABASE_KEY, {
      cookieOptions: SUPABASE_COOKIES,
      cookies: {
        getAll: () => request.cookies.getAll(),
        setAll: (list) => {
          for (const { name, value } of list) request.cookies.set(name, value);
          response = NextResponse.next({ request: { headers } });
          for (const { name, value, options } of list) response.cookies.set(name, value, options);
        },
      },
    });
    // getClaims verifies the access token (refreshing it when needed); never
    // trust getSession() alone for this.
    const { data } = await supabase.auth.getClaims();
    signedIn = !!data?.claims?.sub;
  }

  if (!signedIn && !isPublicPath(path)) {
    const url = request.nextUrl.clone();
    url.pathname = "/login";
    url.search = "";
    if (path !== "/") url.searchParams.set("next", path + request.nextUrl.search);
    response = NextResponse.redirect(url);
  } else if (signedIn && path === "/login") {
    const url = request.nextUrl.clone();
    url.pathname = "/";
    url.search = "";
    response = NextResponse.redirect(url);
  }

  response.headers.set("Content-Security-Policy", csp);
  response.headers.set("X-Content-Type-Options", "nosniff");
  response.headers.set("Referrer-Policy", "same-origin");
  response.headers.set("X-Frame-Options", "DENY");
  response.headers.set("Permissions-Policy", "camera=(), microphone=(), geolocation=()");
  // Behind a TLS proxy (Railway) the scheme arrives in X-Forwarded-Proto.
  if (process.env.NODE_ENV === "production" && (request.headers.get("x-forwarded-proto") ?? request.nextUrl.protocol.replace(":", "")) === "https") {
    response.headers.set("Strict-Transport-Security", "max-age=31536000");
  }
  return response;
}

export const config = {
  matcher: [
    {
      source: "/((?!_next/static|_next/image|favicon.ico|icon.svg).*)",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
};
