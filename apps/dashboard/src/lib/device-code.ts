// The CLI login code shown by `muretto login`: 8 letters from an alphabet with
// no vowels and no look-alikes, displayed as XXXX-XXXX. Mirrors the server.
const ALPHABET = "BCDFGHJKLMNPQRSTVWXZ";

/** Returns "XXXX-XXXX" for a valid code (any case, with or without a dash or space), else null. */
export function normalizeCode(v: string | null | undefined): string | null {
  if (!v || v.length > 16) return null;
  const s = v.toUpperCase().replace(/[-\s]/g, "");
  if (s.length !== 8) return null;
  for (const c of s) if (!ALPHABET.includes(c)) return null;
  return `${s.slice(0, 4)}-${s.slice(4)}`;
}

/** Whole minutes from now until an ISO time, at least 1. */
export function minutesUntil(iso: string, now: number = Date.now()): number {
  return Math.max(1, Math.round((new Date(iso).getTime() - now) / 60_000));
}
