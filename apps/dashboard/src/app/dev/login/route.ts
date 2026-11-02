// DEV ONLY: sign in without Supabase by asking a loopback murettod (started with
// MURETTO_DEV_LOGIN=1) for a token. Returns 404 unless MURETTO_DEV_AUTH=1 in a
// development build.
import { NextResponse, type NextRequest } from "next/server";
import { API_URL, DEV_AUTH, DEV_SAMPLE_COOKIE, DEV_TOKEN_COOKIE } from "@/lib/config";
import { redirectHere, sameOrigin } from "@/lib/http";
import { safeNext } from "@/lib/safe-next";

const cookie = { httpOnly: true, sameSite: "lax" as const, path: "/", secure: false, maxAge: 7 * 86_400 };

export async function POST(request: NextRequest) {
  if (!DEV_AUTH) return new NextResponse("not found", { status: 404 });
  if (!sameOrigin(request)) return new NextResponse("forbidden", { status: 403 });
  const host = new URL(API_URL).hostname;
  if (!["127.0.0.1", "localhost", "::1", "[::1]"].includes(host)) return new NextResponse("dev login needs a loopback MURETTO_API_URL", { status: 400 });

  const res = await fetch(`${API_URL}/v1/dev/token`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ login: "alice", name: "Alice Martin" }),
    cache: "no-store",
  }).catch(() => null);
  if (!res?.ok) return redirectHere("/login?error=dev_api", 303);
  const body = (await res.json()) as { access_token?: string };
  if (!body.access_token?.startsWith("mrt_")) return redirectHere("/login?error=dev_api", 303);

  const form = await request.formData().catch(() => null);
  const next = safeNext(typeof form?.get("next") === "string" ? (form.get("next") as string) : null);
  const out = redirectHere(next, 303);
  out.cookies.delete(DEV_SAMPLE_COOKIE);
  out.cookies.set(DEV_TOKEN_COOKIE, body.access_token, cookie);
  out.cookies.set("mrt_dev_name", "Alice Martin", cookie);
  out.cookies.set("mrt_dev_login", "alice", cookie);
  return out;
}
