"use client";

// Interactive bit of the resource page: Keep / Not needed as two big buttons.
import { useState, useTransition } from "react";
import { decide } from "@/app/(app)/actions";

type State = "keep" | "not_needed" | "pending";

const base = { height: 40, borderRadius: "var(--radius-md)", font: "500 14px var(--font-sans)", cursor: "pointer" } as const;
const on = { ...base, background: "var(--accent-wash)", color: "var(--accent)", border: "1px solid var(--accent-line)" };
const off = { ...base, background: "var(--surface)", color: "var(--ink)", border: "1px solid var(--control-line)" };

/** Keep / Not needed for one resource, full width. Records the decision; Muretto never deletes. */
export function DecideButtons({ resourceId, state, line }: { resourceId: string; state: State; line: string }) {
  const [current, setCurrent] = useState<State>(state);
  const [touched, setTouched] = useState(false);
  const [failed, setFailed] = useState(false);
  const [pending, start] = useTransition();
  function choose(next: State) {
    const target = current === next ? "pending" : next;
    const before = current;
    setCurrent(target);
    setTouched(true);
    setFailed(false);
    start(async () => {
      const ok = await decide(resourceId, target);
      if (!ok) {
        setCurrent(before);
        setFailed(true);
      }
    });
  }
  const shown = failed ? "didn't save, try again" : touched ? (current === "keep" ? "kept · just now" : current === "not_needed" ? "not needed · just now" : line) : line;
  return (
    <>
      <span role={failed ? "alert" : undefined} style={{ marginTop: 4, font: "12.5px var(--font-mono)", color: "var(--ink-faint)" }}>
        {shown}
      </span>
      <div role="group" aria-label="Still need it?" style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8, marginTop: 16 }}>
        <button type="button" aria-pressed={current === "keep"} disabled={pending} onClick={() => choose("keep")} style={current === "keep" ? on : off}>
          Keep
        </button>
        <button type="button" aria-pressed={current === "not_needed"} disabled={pending} onClick={() => choose("not_needed")} style={current === "not_needed" ? on : off}>
          Not needed
        </button>
      </div>
    </>
  );
}
