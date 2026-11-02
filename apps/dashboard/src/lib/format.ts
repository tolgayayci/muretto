// Time and count formatting. Everything takes the clock and time zone as
// arguments so server and client render the same text.
export const DISPLAY_TZ = process.env.MURETTO_DISPLAY_TZ || "UTC";

const DAY = 86_400_000;

function parts(d: Date, tz: string) {
  const f = new Intl.DateTimeFormat("en-US", { timeZone: tz, year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  const o: Record<string, string> = {};
  for (const p of f.formatToParts(d)) o[p.type] = p.value;
  return o;
}

function dayKey(d: Date, tz: string) {
  const p = parts(d, tz);
  return `${p.year}-${p.month}-${p.day}`;
}

export function clock(d: Date, tz = DISPLAY_TZ): string {
  const p = parts(d, tz);
  return `${p.hour}:${p.minute}`;
}

/** "today 14:02", "yesterday", "Oct 29". */
export function when(d: Date, now: Date, tz = DISPLAY_TZ, withTime = true): string {
  if (dayKey(d, tz) === dayKey(now, tz)) return withTime ? `today ${clock(d, tz)}` : "today";
  if (dayKey(d, tz) === dayKey(new Date(now.getTime() - DAY), tz)) return "yesterday";
  const p = parts(d, tz);
  return `${p.month} ${p.day}`;
}

/** "Oct 29". */
export function day(d: Date, tz = DISPLAY_TZ): string {
  const p = parts(d, tz);
  return `${p.month} ${p.day}`;
}

/** "5s ago", "3 min ago", "2 h ago", "2 days ago". */
export function ago(d: Date, now: Date): string {
  const s = Math.max(0, Math.round((now.getTime() - d.getTime()) / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} h ago`;
  const days = Math.round(h / 24);
  return days === 1 ? "1 day ago" : `${days} days ago`;
}

/** "today", "1 day", "11 days". */
export function age(days: number): string {
  if (days <= 0) return "today";
  return days === 1 ? "1 day" : `${days} days`;
}

export function duration(min: number): string {
  if (min < 60) return `${min} min`;
  const h = Math.floor(min / 60);
  const m = min % 60;
  return m ? `${h} h ${m} min` : `${h} h`;
}

const WORDS = ["zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"];

/** "three" for small numbers, digits from ten. */
export function word(n: number): string {
  return n >= 0 && n < WORDS.length ? WORDS[n] : String(n);
}

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** "1 critical · 2 high" from severities. */
export function severitySummary(severities: string[]): string {
  const order = ["critical", "high", "medium", "low", "info"];
  return order
    .map((s) => [s, severities.filter((x) => x === s).length] as const)
    .filter(([, n]) => n > 0)
    .map(([s, n]) => `${n} ${s}`)
    .join(" · ");
}
