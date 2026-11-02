/** The signed-in user's GitHub avatar, or their initials when there is none. */
export function UserAvatar({ initials, url, size }: { initials: string; url?: string; size: number }) {
  const box = { flex: `0 0 ${size}px`, width: size, height: size, borderRadius: 999, boxSizing: "border-box" as const };
  if (url) {
    // eslint-disable-next-line @next/next/no-img-element -- GitHub avatar, allowed by the CSP; no image optimizer needed.
    return <img src={url} alt="" width={size} height={size} style={{ ...box, border: "1px solid var(--line-strong)", objectFit: "cover" }} />;
  }
  return (
    <span aria-hidden style={{ ...box, background: "var(--surface-hover)", border: "1px solid var(--line-strong)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: Math.round(size / 2.9), fontWeight: 600, color: "var(--ink)" }}>
      {initials}
    </span>
  );
}
