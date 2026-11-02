import "server-only";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { DEV_AUTH, DEV_SAMPLE_COOKIE, DEV_TOKEN_COOKIE, supabaseConfigured } from "@/lib/config";
import { isScenario, type Scenario } from "@/lib/sample";
import { createClient } from "@/lib/supabase/server";

export interface ViewerUser {
  /** Display only. Never use it for access decisions. */
  name: string;
  login: string;
  avatarUrl?: string;
}

export type Viewer =
  | { kind: "supabase"; token: string; user: ViewerUser }
  | { kind: "dev"; token: string; user: ViewerUser }
  | { kind: "sample"; scenario: Scenario; user: ViewerUser };

const SAMPLE_USER: ViewerUser = { name: "Alice Martin", login: "alice" };

function clean(v: unknown, max = 100): string {
  return typeof v === "string" ? v.replace(/[^\p{L}\p{N}\p{Zs}._@'-]/gu, "").trim().slice(0, max) : "";
}

/** The signed-in user and the credential to call the API with, or null. */
export async function getViewer(): Promise<Viewer | null> {
  if (DEV_AUTH) {
    const jar = await cookies();
    const scenario = jar.get(DEV_SAMPLE_COOKIE)?.value;
    if (isScenario(scenario)) return { kind: "sample", scenario, user: SAMPLE_USER };
    const token = jar.get(DEV_TOKEN_COOKIE)?.value;
    if (token?.startsWith("mrt_")) return { kind: "dev", token, user: { name: jar.get("mrt_dev_name")?.value ?? "Developer", login: jar.get("mrt_dev_login")?.value ?? "dev" } };
  }
  if (!supabaseConfigured) return null;
  const supabase = await createClient();
  const { data: claims } = await supabase.auth.getClaims();
  if (!claims?.claims?.sub) return null;
  const { data } = await supabase.auth.getSession();
  const token = data.session?.access_token;
  if (!token) return null;
  const meta = (claims.claims.user_metadata ?? {}) as Record<string, unknown>;
  const login = clean(meta.user_name) || clean(meta.preferred_username) || "you";
  const avatar = typeof meta.avatar_url === "string" && meta.avatar_url.startsWith("https://avatars.githubusercontent.com/") ? meta.avatar_url : undefined;
  return { kind: "supabase", token, user: { name: clean(meta.full_name) || clean(meta.name) || login, login, avatarUrl: avatar } };
}

export async function requireViewer(): Promise<Viewer> {
  const v = await getViewer();
  if (!v) redirect("/login");
  return v;
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  return (parts[0][0] + (parts.length > 1 ? parts[parts.length - 1][0] : "")).toUpperCase();
}
