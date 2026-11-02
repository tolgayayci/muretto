"use client";

import { Icon } from "@/components/ds";
import { RetryButton } from "@/components/client";

export default function SettingsError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <div className="m-center">
      <div role="alert" className="m-card m-alert">
        <span className="m-tile" style={{ width: 44, height: 44, borderRadius: 12, color: "var(--ink)" }}>
          <Icon name="wifioff" size={20} />
        </span>
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <h1>
            <span>We can&apos;t load your settings.</span>
            <span className="q">Nothing changed on your machines.</span>
          </h1>
          <span style={{ fontSize: 15, lineHeight: "23px", color: "var(--ink-muted)" }}>
            The dashboard couldn&apos;t reach Muretto. Sync settings live in each CLI, so <span className="mono" style={{ color: "var(--ink)" }}>muretto sync off</span> still works in a terminal.
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
