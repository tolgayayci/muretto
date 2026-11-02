"use client";

import { Icon } from "@/components/ds";
import { CommandChip, RetryButton } from "@/components/client";

export default function ProvidersError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <div className="m-center">
      <div role="alert" className="m-card m-alert" style={{ maxWidth: 480, borderRadius: "var(--radius-2xl)" }}>
        <span className="m-tile" style={{ width: 44, height: 44, borderRadius: 12, color: "var(--ink)" }}>
          <Icon name="wifioff" size={20} />
        </span>
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <h1>
            <span>We can&apos;t load your providers.</span>
            <span className="q">Nothing changed in your accounts.</span>
          </h1>
          <span style={{ fontSize: 15, lineHeight: "23px", color: "var(--ink-muted)" }}>The dashboard couldn&apos;t reach Muretto. Your CLI still checks on its own. Run this in a terminal:</span>
        </div>
        <CommandChip command="muretto providers --coverage" />
        <div className="m-full">
          <RetryButton reset={reset} />
        </div>
        {error.digest ? <span style={{ paddingTop: 16, borderTop: "1px solid var(--line)", font: "12.5px var(--font-mono)", color: "var(--ink-faint)" }}>error {error.digest}</span> : null}
      </div>
    </div>
  );
}
