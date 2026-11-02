import { createBrowserClient } from "@supabase/ssr";
import { SUPABASE_COOKIES, SUPABASE_KEY, SUPABASE_URL } from "@/lib/config";

/** Supabase client for the browser. Used only to start the GitHub sign-in. */
export function createClient() {
  return createBrowserClient(SUPABASE_URL, SUPABASE_KEY, { cookieOptions: SUPABASE_COOKIES });
}
