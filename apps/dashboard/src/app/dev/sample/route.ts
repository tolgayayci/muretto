// DEV ONLY: browse the dashboard with built-in sample data, one scenario per
// state (/dev/sample?scenario=many&next=/resources). 404 outside dev builds.
import { NextResponse, type NextRequest } from "next/server";
import { DEV_AUTH, DEV_SAMPLE_COOKIE, DEV_TOKEN_COOKIE } from "@/lib/config";
import { redirectHere } from "@/lib/http";
import { safeNext } from "@/lib/safe-next";
import { isScenario } from "@/lib/sample";

export async function GET(request: NextRequest) {
  if (!DEV_AUTH) return new NextResponse("not found", { status: 404 });
  const scenario = request.nextUrl.searchParams.get("scenario") ?? "normal";
  if (!isScenario(scenario)) return new NextResponse("unknown scenario", { status: 400 });
  const out = redirectHere(safeNext(request.nextUrl.searchParams.get("next")));
  out.cookies.delete(DEV_TOKEN_COOKIE);
  out.cookies.set(DEV_SAMPLE_COOKIE, scenario, { httpOnly: true, sameSite: "lax", path: "/", secure: false });
  return out;
}
