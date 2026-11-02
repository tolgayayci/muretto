// Generated session avatars: two soft blobs on a dark stone, picked from the
// session id so the same session always looks the same.
const PALETTE = ["#9fb7a0", "#d9c49a", "#8fa6c4", "#b3a4cf", "#7fbfb3", "#c9b8a6", "#a9c27a", "#9ab0b8"];

function hash(s: string): number {
  let h = 7;
  for (const c of s) h = (Math.imul(h, 31) + c.charCodeAt(0)) >>> 0;
  return h;
}

export function blobBackground(id: string): string {
  const h = hash(id);
  const a = PALETTE[h % PALETTE.length];
  const b = PALETTE[(h >>> 3) % PALETTE.length];
  const x = 20 + (h % 50);
  const y = 15 + ((h >>> 5) % 45);
  return `radial-gradient(circle at ${x}% ${y}%, ${a} 0%, transparent 62%), radial-gradient(circle at ${100 - x}% ${100 - y / 2}%, ${b} 0%, transparent 58%), #23231f`;
}
