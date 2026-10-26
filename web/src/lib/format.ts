// Formatting helpers. Pure functions of their inputs (including `now` and the
// time zone), so server and client render the same text.

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

export function ms(iso: string | undefined): number {
  return iso ? Date.parse(iso) : NaN;
}

/** "just now", "2 min ago", "3 h ago", "6 days ago"; future: "in 3 days". */
export function timeAgo(iso: string | undefined, now: number): string {
  const t = ms(iso);
  if (Number.isNaN(t)) return "never";
  const d = now - t;
  const abs = Math.abs(d);
  let s: string;
  if (abs < 45_000) return "just now";
  if (abs < HOUR) s = `${Math.round(abs / MIN)} min`;
  else if (abs < DAY) s = `${Math.round(abs / HOUR)} h`;
  else {
    const days = Math.round(abs / DAY);
    s = `${days} ${days === 1 ? "day" : "days"}`;
  }
  return d >= 0 ? `${s} ago` : `in ${s}`;
}

/** Whole days from now until iso (negative when past). */
export function daysUntil(iso: string, now: number): number {
  return Math.floor((ms(iso) - now) / DAY);
}

function parts(iso: string, timeZone: string) {
  const f = new Intl.DateTimeFormat("en-GB", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  });
  const o: Record<string, string> = {};
  for (const p of f.formatToParts(new Date(iso))) o[p.type] = p.value;
  return { ymd: `${o.year}-${o.month}-${o.day}`, hm: `${o.hour}:${o.minute}` };
}

/** "14:02" in the given time zone. */
export function clock(iso: string, timeZone: string): string {
  return parts(iso, timeZone).hm;
}

/** "today", "yesterday", "Oct 18". */
export function dayLabel(iso: string, now: number, timeZone: string): string {
  const a = parts(iso, timeZone).ymd;
  const today = parts(new Date(now).toISOString(), timeZone).ymd;
  const yesterday = parts(new Date(now - DAY).toISOString(), timeZone).ymd;
  if (a === today) return "today";
  if (a === yesterday) return "yesterday";
  return new Intl.DateTimeFormat("en-US", { timeZone, month: "short", day: "numeric" }).format(new Date(iso));
}

/** "today 14:02–14:31", or "today 14:02–now" for a session still running. */
export function sessionSpan(startIso: string, endIso: string | undefined, now: number, timeZone: string): string {
  const start = `${dayLabel(startIso, now, timeZone)} ${clock(startIso, timeZone)}`;
  if (!endIso) return `${start}–now`;
  const sameDay = dayLabel(endIso, now, timeZone) === dayLabel(startIso, now, timeZone);
  return `${start}–${sameDay ? clock(endIso, timeZone) : `${dayLabel(endIso, now, timeZone)} ${clock(endIso, timeZone)}`}`;
}

/** Full timestamp for tooltips: "2026-10-26 14:02 UTC". */
export function stamp(iso: string | undefined, timeZone: string): string {
  if (!iso) return "never";
  const p = parts(iso, timeZone);
  const tz =
    timeZone === "UTC"
      ? "UTC"
      : (new Intl.DateTimeFormat("en-US", { timeZone, timeZoneName: "short" })
          .formatToParts(new Date(iso))
          .find((x) => x.type === "timeZoneName")?.value ?? "");
  return `${p.ymd} ${p.hm} ${tz}`.trim();
}

/** Money from integer micros. Small amounts keep enough precision to be non-zero. */
export function money(micros: number, currency = "USD"): string {
  const amount = micros / 1_000_000;
  const digits = amount !== 0 && Math.abs(amount) < 0.1 ? 4 : 2;
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency,
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(amount);
}

/** First 4 characters of a session id, like a short commit hash. */
export function shortId(id: string): string {
  return id.replace(/-/g, "").slice(0, 4);
}

const KIND_LABELS: Record<string, string> = {
  env_var: "env var",
};

/** "supabase.table" -> "table" */
export function kindLabel(kind: string): string {
  const k = kind.slice(kind.indexOf(".") + 1);
  return KIND_LABELS[k] ?? k.replace(/_/g, " ");
}

const PROVIDERS: Record<string, string> = { supabase: "Supabase", vercel: "Vercel" };

export function providerLabel(p: string): string {
  return PROVIDERS[p] ?? p.charAt(0).toUpperCase() + p.slice(1);
}

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}
