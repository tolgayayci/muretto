"use client";

import { useEffect, useRef, useState } from "react";

/** Small Copy button for a fix. The text stays visible to copy by hand. */
export function CopyButton({ text, label = "Copy" }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // Clipboard can be blocked.
    }
    setCopied(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), 1600);
  }
  return (
    <button
      type="button"
      onClick={copy}
      aria-label={`${label}: ${text}`}
      style={{ height: 28, padding: "0 10px", border: "1px solid var(--line-strong)", borderRadius: 7, background: "var(--surface)", color: "var(--ink-muted)", font: "500 12.5px var(--font-sans)", cursor: "pointer" }}
    >
      {copied ? "Copied" : label}
    </button>
  );
}
