// Signs out of the dashboard on this browser. Machines keep syncing.
import { NextResponse, type NextRequest } from "next/server";
import { DEV_AUTH, DEV_SAMPLE_COOKIE, DEV_TOKEN_COOKIE, supabaseConfigured } from "@/lib/config";
import { redirectHere, sameOrigin } from "@/lib/http";
import { createClient } from "@/lib/supabase/server";

async function signOut(reason?: string) {
  if (supabaseConfigured) {
    const supabase = await createClient();
    await supabase.auth.signOut({ scope: "local" });
  }
  const res = redirectHere(reason ? `/login?error=${reason}` : "/login", 303);
  if (DEV_AUTH) {
    for (const name of [DEV_TOKEN_COOKIE, DEV_SAMPLE_COOKIE, "mrt_dev_name", "mrt_dev_login"]) res.cookies.delete(name);
  }
  return res;
}

export async function POST(request: NextRequest) {
  // Same-origin form posts only.
  if (!sameOrigin(request)) return new NextResponse("forbidden", { status: 403 });
  return signOut();
}

/** Used when the API says the session expired. */
export async function GET(request: NextRequest) {
  return signOut(request.nextUrl.searchParams.get("reason") === "expired" ? "expired" : undefined);
}
