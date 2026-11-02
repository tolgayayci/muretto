"use client";

// Small interactive pieces: copy buttons, Keep / Not needed, retry.
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, useTransition } from "react";
import { decide } from "@/app/(app)/actions";

export function CommandChip({ command, small = false, prompt = true }: { command: string; small?: boolean; prompt?: boolean }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  async function copy() {
    try {
      await navigator.clipboard.writeText(command);
    } catch {
      // Clipboard can be blocked; the command is still visible to copy by hand.
    }
    setCopied(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), 1600);
  }
  return (
    <span className={`m-cmd${small ? " sm" : ""}`}>
      <code>
        {prompt ? <span className="p">$</span> : null}
        {command}
      </code>
      <button type="button" onClick={copy} aria-label={`Copy ${command}`}>
        {copied ? "Copied" : "Copy"}
      </button>
    </span>
  );
}

export type DecisionState = "keep" | "not_needed" | "pending";

/** Keep / Not needed for one resource. Records the decision; Muretto never deletes. */
export function KeepToggle({ resourceId, state }: { resourceId: string; state: DecisionState }) {
  const [current, setCurrent] = useState<DecisionState>(state);
  const [pending, start] = useTransition();
  const [failed, setFailed] = useState(false);
  function choose(next: DecisionState) {
    const target = current === next ? "pending" : next;
    const before = current;
    setCurrent(target);
    setFailed(false);
    start(async () => {
      const ok = await decide(resourceId, target);
      if (!ok) {
        setCurrent(before);
        setFailed(true);
      }
    });
  }
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
      {failed ? (
        <span role="alert" style={{ font: "12px var(--font-mono)", color: "var(--ink-faint)" }}>
          didn&apos;t save
        </span>
      ) : null}
      <span role="group" aria-label="Still need it?" className="m-seg">
        <button type="button" aria-pressed={current === "keep"} disabled={pending} onClick={() => choose("keep")}>
          Keep
        </button>
        <button type="button" aria-pressed={current === "not_needed"} disabled={pending} onClick={() => choose("not_needed")}>
          Not needed
        </button>
      </span>
    </span>
  );
}

export function RetryButton({ reset, label = "Try again" }: { reset: () => void; label?: string }) {
  const router = useRouter();
  const [pending, start] = useTransition();
  return (
    <button
      type="button"
      className="mu-btn mu-btn-primary"
      disabled={pending}
      onClick={() =>
        start(() => {
          router.refresh();
          reset();
        })
      }
    >
      {label}
    </button>
  );
}
