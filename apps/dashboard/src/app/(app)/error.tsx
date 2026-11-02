"use client";

import { Icon } from "@/components/ds";
import { RetryButton } from "@/components/client";

export default function PageError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <div className="m-center">
      <div role="alert" className="m-card m-alert">
        <span className="m-tile" style={{ width: 44, height: 44, borderRadius: 12, color: "var(--ink)" }}>
          <Icon name="wifioff" size={20} />
        </span>
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <h1>
            <span>We can&apos;t load this page.</span>
            <span className="q">Nothing changed in your accounts.</span>
          </h1>
          <span style={{ fontSize: 15, lineHeight: "23px", color: "var(--ink-muted)" }}>
            The dashboard couldn&apos;t reach Muretto. Your CLI still works on its own: run <span className="mono" style={{ color: "var(--ink)" }}>muretto</span> in a terminal.
          </span>
        </div>
        <div className="m-full">
          <RetryButton reset={reset} />
        </div>
        {error.digest ? <span style={{ paddingTop: 16, borderTop: "1px solid var(--line)", font: "12.5px var(--font-mono)", color: "var(--ink-faint)" }}>error {error.digest}</span> : null}
      </div>
    </div>
  );
}
