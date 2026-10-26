"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";

// ------------------------------------------------------------------ brand

/** A muretto is a low wall: two courses of staggered bricks. */
export function Mark({ className = "size-5" }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden="true">
      <g fill="currentColor">
        <rect x="1" y="9" width="10" height="5.5" rx="1" />
        <rect x="13" y="9" width="10" height="5.5" rx="1" />
        <rect x="1" y="16.5" width="4" height="5.5" rx="1" />
        <rect x="7" y="16.5" width="10" height="5.5" rx="1" />
        <rect x="19" y="16.5" width="4" height="5.5" rx="1" />
        <rect x="7" y="2" width="10" height="5" rx="1" opacity="0.4" />
      </g>
    </svg>
  );
}

export function Wordmark({ className = "", compact = false }: { className?: string; compact?: boolean }) {
  return (
    <span className={`inline-flex items-center gap-2 ${className}`}>
      <Mark className="size-5 text-accent" />
      <span className={`text-[15px] font-semibold tracking-[-0.01em] ${compact ? "hidden sm:inline" : ""}`}>muretto</span>
    </span>
  );
}

// ------------------------------------------------------------------ icons

type IconName =
  | "chevron"
  | "copy"
  | "check"
  | "alert"
  | "x"
  | "external"
  | "github"
  | "shield"
  | "lock"
  | "code"
  | "laptop"
  | "toggle"
  | "users"
  | "clock"
  | "plus"
  | "question";

const PATHS: Record<IconName, ReactNode> = {
  chevron: <path d="m9 6 6 6-6 6" />,
  copy: (
    <>
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15V6a2 2 0 0 1 2-2h8" />
    </>
  ),
  check: <path d="m5 12.5 4.5 4.5L19 7.5" />,
  alert: (
    <>
      <path d="M12 4 2.8 19.5h18.4Z" />
      <path d="M12 10v4.5M12 17.2v.3" />
    </>
  ),
  x: <path d="m6 6 12 12M18 6 6 18" />,
  external: <path d="M14 5h5v5M19 5l-8 8M18 14v4a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h4" />,
  github: (
    <path d="M9 19c-4 1.4-4-2-6-2.5m12 5v-3.5c0-1 .1-1.4-.5-2 2.8-.3 5.5-1.4 5.5-6a4.6 4.6 0 0 0-1.3-3.2 4.2 4.2 0 0 0-.1-3.2s-1.1-.3-3.5 1.3a12 12 0 0 0-6.2 0C6.5 2.8 5.4 3.1 5.4 3.1a4.2 4.2 0 0 0-.1 3.2A4.6 4.6 0 0 0 4 9.5c0 4.6 2.7 5.7 5.5 6-.6.6-.6 1.2-.5 2V21" />
  ),
  shield: <path d="M12 3 4.5 6v5.5c0 4.6 3.2 8.2 7.5 9.5 4.3-1.3 7.5-4.9 7.5-9.5V6Z" />,
  lock: (
    <>
      <rect x="5" y="10.5" width="14" height="10" rx="2" />
      <path d="M8.5 10.5V8a3.5 3.5 0 0 1 7 0v2.5" />
    </>
  ),
  code: <path d="m8 7-5 5 5 5M16 7l5 5-5 5M13.5 4.5l-3 15" />,
  laptop: (
    <>
      <rect x="4.5" y="5" width="15" height="10.5" rx="1.5" />
      <path d="M2.5 19h19" />
    </>
  ),
  toggle: (
    <>
      <rect x="2.5" y="7" width="19" height="10" rx="5" />
      <circle cx="16.5" cy="12" r="2.6" />
    </>
  ),
  users: (
    <>
      <circle cx="9" cy="8.5" r="3.2" />
      <path d="M3 19.5c.6-3.2 3-5 6-5s5.4 1.8 6 5M15.5 5.5a3.2 3.2 0 0 1 0 6.2M18 14.8c1.6.7 2.7 2.4 3 4.7" />
    </>
  ),
  clock: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 7.5V12l3 2" />
    </>
  ),
  plus: <path d="M12 5v14M5 12h14" />,
  question: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M9.6 9.5a2.5 2.5 0 0 1 4.8.9c0 1.7-2.4 2.1-2.4 3.6M12 16.8v.3" />
    </>
  ),
};

export function Icon({ name, className = "size-4" }: { name: IconName; className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={className}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.7}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {PATHS[name]}
    </svg>
  );
}

// ------------------------------------------------------------------ copy

export function CopyButton({
  text,
  label = "Copy",
  className = "",
  variant = "ghost",
}: {
  text: string;
  label?: string;
  className?: string;
  variant?: "ghost" | "solid";
}) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(false), 1600);
    } catch {
      /* clipboard blocked: the text is still selectable */
    }
  }

  const base =
    variant === "solid"
      ? "border border-line-strong bg-panel-2 hover:bg-hover text-fg"
      : "text-muted hover:text-fg hover:bg-hover";
  return (
    <button
      type="button"
      onClick={copy}
      className={`inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2 text-xs font-medium transition-colors ${base} ${className}`}
      aria-label={copied ? "Copied" : `${label}: ${text.length > 60 ? text.slice(0, 60) + "…" : text}`}
    >
      <Icon name={copied ? "check" : "copy"} className={`size-3.5 ${copied ? "text-accent" : ""}`} />
      <span aria-hidden="true" className="w-[42px] text-left">
        {copied ? "Copied" : label}
      </span>
      <span role="status" className="sr-only">
        {copied ? "Copied to clipboard" : ""}
      </span>
    </button>
  );
}

// ------------------------------------------------------------------ labels

/** The attribution label. Always shown, never a guess. */
export function AttributionLabel({ label, className = "" }: { label?: "exact" | "unknown"; className?: string }) {
  if (label === "exact") {
    return (
      <span
        className={`inline-flex h-5 items-center rounded border border-accent-line px-1.5 font-mono text-2xs text-accent ${className}`}
        title="exact: a session's tool result contains this resource's id. Evidence is stored."
      >
        exact
      </span>
    );
  }
  return (
    <span
      className={`inline-flex h-5 items-center whitespace-nowrap rounded border border-dashed border-line-strong px-1.5 font-mono text-2xs text-muted ${className}`}
      title="No recorded agent session created this. Muretto never guesses."
    >
      creator unknown
    </span>
  );
}

export function SampleBadge({ className = "" }: { className?: string }) {
  return (
    <span
      className={`inline-flex h-6 shrink-0 items-center gap-1.5 rounded-full whitespace-nowrap border border-dashed border-accent-line bg-accent-wash px-2.5 text-xs font-medium text-accent ${className}`}
      title="Fake data built into the app (acme, alice, bob…). Nothing here comes from a real account."
    >
      <span className="size-1.5 rounded-full bg-accent" aria-hidden="true" />
      Sample data
    </span>
  );
}

export function Kbd({ children }: { children: ReactNode }) {
  return (
    <kbd className="inline-flex h-[18px] min-w-[18px] items-center justify-center rounded border border-line-strong bg-panel-2 px-1 font-mono text-[10px] text-muted">
      {children}
    </kbd>
  );
}

export function Panel({
  title,
  aside,
  children,
  className = "",
  id,
  tone = "default",
}: {
  title: ReactNode;
  aside?: ReactNode;
  children: ReactNode;
  className?: string;
  id?: string;
  tone?: "default" | "danger";
}) {
  const headingId = id ? `${id}-title` : undefined;
  return (
    <section
      aria-labelledby={headingId}
      className={`flex min-w-0 flex-col rounded-xl border bg-panel shadow-panel ${
        tone === "danger" ? "border-danger-line" : "border-line"
      } ${className}`}
    >
      <header className="flex h-11 shrink-0 items-center justify-between gap-3 border-b border-line px-4">
        <h2 id={headingId} className="flex items-center gap-2 text-[13px] font-semibold tracking-[-0.005em]">
          {title}
        </h2>
        {aside ? <div className="flex items-center gap-2 text-xs text-muted">{aside}</div> : null}
      </header>
      {children}
    </section>
  );
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T;
  options: { value: T; label: string; disabled?: boolean; hint?: string }[];
  onChange: (v: T) => void;
  label: string;
}) {
  return (
    <div role="radiogroup" aria-label={label} className="inline-flex h-8 items-center rounded-lg border border-line bg-sunken p-0.5">
      {options.map((o) => {
        const active = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={active}
            disabled={o.disabled}
            title={o.hint}
            onClick={() => onChange(o.value)}
            className={`h-full rounded-md px-3 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${
              active ? "bg-panel-2 text-fg shadow-[0_0_0_1px_var(--line-strong)]" : "text-muted hover:text-fg"
            }`}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
