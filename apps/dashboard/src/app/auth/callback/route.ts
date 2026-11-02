// GitHub OAuth (through Supabase Auth, PKCE) lands here with a one-time code.
import type { NextRequest } from "next/server";
import { redirectHere } from "@/lib/http";
import { safeNext } from "@/lib/safe-next";
import { createClient } from "@/lib/supabase/server";

export async function GET(request: NextRequest) {
  const url = request.nextUrl;
  const code = url.searchParams.get("code");
  const next = safeNext(url.searchParams.get("next"));
  const fail = (reason: string) => redirectHere(`/login?error=${reason}`);

  if (url.searchParams.get("error")) return fail("denied");
  if (!code) return fail("missing_code");

  const supabase = await createClient();
  const { error } = await supabase.auth.exchangeCodeForSession(code);
  if (error) return fail("exchange_failed");
  return redirectHere(next);
}
