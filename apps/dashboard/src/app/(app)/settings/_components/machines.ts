// What the settings page can honestly say about a signed-in CLI. Muretto only
// knows each CLI token's name, when it was signed in and when it last called
// the API, so freshness is derived from last_used_at and nothing else.
import type { Machine } from "@/lib/api";
import { ago, day } from "@/lib/format";

const DAY = 86_400_000;
/** No sync for this long and the machine gets a quiet "needs a look". */
export const STALE_AFTER_MS = 2 * DAY;

export interface MachineView {
  id: string;
  name: string;
  signedIn: string;
  /** "5s ago", "2 days ago", or null when it never synced. */
  lastSync: string | null;
  lastSyncDay: string | null;
  state: "ok" | "stale" | "never";
  /** Whole days since the last sync, for "hasn't synced in N days". */
  quietDays: number;
}

function parse(s: string | undefined): Date | null {
  if (!s) return null;
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? null : d;
}

export function machineView(m: Machine, now: Date): MachineView {
  const created = parse(m.created_at);
  const last = parse(m.last_used_at);
  const quiet = last ? now.getTime() - last.getTime() : 0;
  return {
    id: m.id,
    name: m.name || "Unnamed machine",
    signedIn: created ? day(created) : "unknown",
    lastSync: last ? ago(last, now) : null,
    lastSyncDay: last ? day(last) : null,
    state: !last ? "never" : quiet >= STALE_AFTER_MS ? "stale" : "ok",
    quietDays: Math.floor(quiet / DAY),
  };
}

/** Most recently synced first; never-synced last. */
export function machineViews(list: Machine[], now: Date): MachineView[] {
  const t = (m: Machine) => parse(m.last_used_at)?.getTime() ?? -Infinity;
  return [...list].sort((a, b) => t(b) - t(a)).map((m) => machineView(m, now));
}
