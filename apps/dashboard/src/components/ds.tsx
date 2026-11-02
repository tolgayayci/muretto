// Muretto design-system components, ported from the design system bundle so the
// markup and mu-* classes match it exactly.
import type { AnchorHTMLAttributes, ButtonHTMLAttributes, ReactNode } from "react";
import Link from "next/link";

const STONES: [number, number, number, number][] = [
  [4, 13, 11, 6],
  [17, 13, 11, 6],
  [4, 21, 4.5, 6],
  [10.5, 21, 11, 6],
  [23.5, 21, 4.5, 6],
];

export function Mark({ size = 24, wordmark = false, label }: { size?: number; wordmark?: boolean; label?: string }) {
  const svg = (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      className="mu-mark"
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      <g fill="currentColor">
        {STONES.map(([x, y, w, h], i) => (
          <rect key={i} x={x} y={y} width={w} height={h} rx={1.2} />
        ))}
        <rect x={10.5} y={5} width={11} height={6} rx={1.2} opacity={0.4} />
      </g>
    </svg>
  );
  if (!wordmark) return svg;
  return (
    <span className="mu-lockup">
      {svg}
      <span className="mu-wordmark">muretto</span>
    </span>
  );
}

type IconPart = ["path" | "circle" | "rect", Record<string, string | number>];

const ICONS: Record<string, IconPart[]> = {
  star: [["path", { d: "M12 3.2l2.7 5.5 6 .9-4.35 4.25 1.03 6-5.38-2.83-5.38 2.83 1.03-6L3.3 9.6l6-.9z" }]],
  "arrow-right": [["path", { d: "M5 12h14M13 6l6 6-6 6" }]],
  external: [["path", { d: "M14 5h5v5M19 5l-8 8M18 14v4a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h4" }]],
  check: [["path", { d: "M5 12.5l4.2 4.2L19 7" }]],
  "check-circle": [["circle", { cx: 12, cy: 12, r: 8.5 }], ["path", { d: "M8.2 12.2l2.6 2.6 5-5.4" }]],
  lock: [["rect", { x: 4.5, y: 10.5, width: 15, height: 10, rx: 2.5 }], ["path", { d: "M8 10.5V7.5a4 4 0 0 1 8 0v3" }]],
  key: [["circle", { cx: 8, cy: 15, r: 4 }], ["path", { d: "M11 12l9-9M16.5 6.5l2.5 2.5M14 9l2 2" }]],
  file: [["path", { d: "M7 3h7l5 5v13H7z" }], ["path", { d: "M14 3v5h5" }], ["path", { d: "M10 13h6M10 17h6" }]],
  search: [["circle", { cx: 11, cy: 11, r: 6.5 }], ["path", { d: "M16 16l4.5 4.5" }]],
  commit: [["circle", { cx: 12, cy: 12, r: 3.6 }], ["path", { d: "M2.5 12h6M15.6 12h5.9" }]],
  terminal: [["rect", { x: 3, y: 4.5, width: 18, height: 15, rx: 2.5 }], ["path", { d: "M7 10l3 2.5L7 15M12.5 15H17" }]],
  laptop: [["rect", { x: 3.5, y: 5, width: 17, height: 11, rx: 2 }], ["path", { d: "M2 19.5h20" }]],
  code: [["path", { d: "M8.5 7L3.5 12l5 5M15.5 7l5 5-5 5" }]],
  sparkle: [["path", { d: "M12 3v4M12 17v4M3 12h4M17 12h4M6 6l2.5 2.5M15.5 15.5L18 18M6 18l2.5-2.5M15.5 8.5L18 6" }]],
  copy: [
    ["rect", { x: 8, y: 8, width: 12, height: 12, rx: 2 }],
    ["path", { d: "M16 8V5.5A1.5 1.5 0 0 0 14.5 4h-9A1.5 1.5 0 0 0 4 5.5v9A1.5 1.5 0 0 0 5.5 16H8" }],
  ],
  play: [["path", { d: "M8 5.5v13l11-6.5z", fill: "currentColor", stroke: "none" }]],
  // Dashboard additions, same 24px grid and stroke.
  globe: [["circle", { cx: 12, cy: 12, r: 8.5 }], ["path", { d: "M3.5 12h17M12 3.5c2.5 2.6 3.5 5.4 3.5 8.5s-1 5.9-3.5 8.5c-2.5-2.6-3.5-5.4-3.5-8.5s1-5.9 3.5-8.5z" }]],
  clock: [["circle", { cx: 12, cy: 12, r: 8.5 }], ["path", { d: "M12 7.5V12l3 2" }]],
  question: [["circle", { cx: 12, cy: 12, r: 8.5 }], ["path", { d: "M9.6 9.4a2.5 2.5 0 0 1 4.8.9c0 1.7-2.4 2.2-2.4 3.7M12 16.8v.2" }]],
  shield: [["path", { d: "M12 3.5 19.5 6v5.5c0 4.5-3.2 8-7.5 9-4.3-1-7.5-4.5-7.5-9V6z" }], ["path", { d: "M12 8.5v4M12 15.5v.5" }]],
  users: [
    ["circle", { cx: 9, cy: 8, r: 3.5 }],
    ["path", { d: "M2.5 20c.6-3.5 3.3-5.5 6.5-5.5s5.9 2 6.5 5.5" }],
    ["path", { d: "M15.5 4.8a3.5 3.5 0 0 1 0 6.4M18 14.8c1.9.7 3.2 2.5 3.5 5.2" }],
  ],
  overview: [
    ["rect", { x: 3.5, y: 3.5, width: 7, height: 8.5, rx: 1.5 }],
    ["rect", { x: 13.5, y: 3.5, width: 7, height: 5, rx: 1.5 }],
    ["rect", { x: 13.5, y: 11.5, width: 7, height: 9, rx: 1.5 }],
    ["rect", { x: 3.5, y: 15, width: 7, height: 5.5, rx: 1.5 }],
  ],
  database: [
    ["path", { d: "M4 5.5c0-1.55 3.6-2.8 8-2.8s8 1.25 8 2.8-3.6 2.8-8 2.8-8-1.25-8-2.8z" }],
    ["path", { d: "M4 5.5v13c0 1.55 3.6 2.8 8 2.8s8-1.25 8-2.8v-13M4 12c0 1.55 3.6 2.8 8 2.8s8-1.25 8-2.8" }],
  ],
  sliders: [["path", { d: "M4 7h10M18 7h2M4 17h4M12 17h8" }], ["circle", { cx: 16, cy: 7, r: 2 }], ["circle", { cx: 10, cy: 17, r: 2 }]],
  updown: [["path", { d: "m7 15 5 5 5-5M7 9l5-5 5 5" }]],
  x: [["path", { d: "M6 6l12 12M18 6 6 18" }]],
  wifioff: [["path", { d: "M2.5 8.5a14 14 0 0 1 19 0M5.5 12a9.5 9.5 0 0 1 13 0M8.8 15.4a5 5 0 0 1 6.4 0M12 19h.01M3 3l18 18" }]],
  plug: [["path", { d: "M9 2v5M15 2v5M6 7h12v4a6 6 0 0 1-12 0V7ZM12 17v5" }]],
};

export type IconName = keyof typeof ICONS;

export function Icon({
  name,
  size = 20,
  strokeWidth = 1.8,
  label,
  tile = false,
}: {
  name: IconName;
  size?: number;
  strokeWidth?: number;
  label?: string;
  tile?: boolean;
}) {
  const parts = ICONS[name] ?? ICONS.sparkle;
  const svg = (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="mu-icon"
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      {parts.map(([tag, attrs], i) => {
        const Tag = tag;
        return <Tag key={i} {...attrs} />;
      })}
    </svg>
  );
  if (!tile) return svg;
  return <span className="mu-icon-tile">{svg}</span>;
}

type ButtonBase = { variant?: "primary" | "secondary" | "link"; size?: "md" | "sm"; icon?: IconName; className?: string; children?: ReactNode };

export function Button({
  variant = "primary",
  size = "md",
  icon,
  className,
  children,
  href,
  ...rest
}: ButtonBase & ({ href: string } & Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href">) | ButtonBase & ({ href?: undefined } & ButtonHTMLAttributes<HTMLButtonElement>)) {
  const cls = ["mu-btn", `mu-btn-${variant}`, size === "sm" ? "mu-btn-sm" : "", className ?? ""].filter(Boolean).join(" ");
  const iconEl = icon ? <Icon name={icon} size={16} /> : null;
  if (href !== undefined) {
    const external = /^https?:\/\//.test(href);
    if (external) {
      return (
        <a href={href} className={cls} rel="noopener noreferrer" target="_blank" {...(rest as AnchorHTMLAttributes<HTMLAnchorElement>)}>
          {iconEl}
          {children}
        </a>
      );
    }
    return (
      <Link href={href} className={cls} {...(rest as Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href">)}>
        {iconEl}
        {children}
      </Link>
    );
  }
  return (
    <button type="button" className={cls} {...(rest as ButtonHTMLAttributes<HTMLButtonElement>)}>
      {iconEl}
      {children}
    </button>
  );
}

const BADGE_TEXT = { exposed: "Open to anyone", review: "Waiting on you", unknown: "Origin unknown", neutral: "" } as const;

export function Badge({ tone = "neutral", children }: { tone?: keyof typeof BADGE_TEXT; children?: ReactNode }) {
  return <span className={`mu-badge mu-badge-${tone}`}>{children ?? BADGE_TEXT[tone]}</span>;
}

export function CheckList({ items }: { items: ReactNode[] }) {
  return (
    <ul className="mu-checks">
      {items.map((it, k) => (
        <li key={k}>
          <span className="mu-ck" aria-hidden>
            <Icon name="check" size={12} strokeWidth={2.6} />
          </span>
          <span>{it}</span>
        </li>
      ))}
    </ul>
  );
}
