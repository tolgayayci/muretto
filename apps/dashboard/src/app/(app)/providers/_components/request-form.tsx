"use client";

// "Use something else?": sends one provider name. There is no public request
// list, so the confirmation only says the request arrived.
import { useState, useTransition, type FormEvent } from "react";
import { requestProviderAction } from "@/app/(app)/actions";

const MAX = 60;
const READ_TODAY: Record<string, string> = { supabase: "Supabase", vercel: "Vercel" };

export function RequestForm() {
  const [value, setValue] = useState("");
  const [sent, setSent] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pending, start] = useTransition();

  function submit(e: FormEvent) {
    e.preventDefault();
    const name = value.trim().replace(/\s+/g, " ");
    if (name.length < 2) {
      setError("Type a provider name.");
      return;
    }
    if (name.length > MAX) {
      setError(`Keep it under ${MAX} characters.`);
      return;
    }
    const known = READ_TODAY[name.toLowerCase()];
    if (known) {
      setError(`Muretto already reads ${known}. Run muretto init to connect it.`);
      return;
    }
    setError(null);
    start(async () => {
      const ok = await requestProviderAction(name);
      if (ok) {
        setSent(name);
        setValue("");
      } else {
        setError("That didn't send. Try again in a moment.");
      }
    });
  }

  if (sent) {
    return (
      <div role="status" style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <span style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
          <span style={{ flex: "0 0 32px", height: 32, boxSizing: "border-box", borderRadius: 999, background: "var(--accent-wash)", border: "1px solid var(--accent-line)", color: "var(--accent)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 15 }}>✓</span>
          <span style={{ display: "flex", flexDirection: "column", gap: 4, minWidth: 0 }}>
            <span style={{ fontSize: 15, fontWeight: 600, overflowWrap: "anywhere" }}>Thanks. We got your request for {sent}.</span>
            <span style={{ fontSize: 14, lineHeight: "21px", color: "var(--ink-muted)" }}>Every request is read. Requests decide which provider Muretto learns to read next. No dates, no promises.</span>
          </span>
        </span>
        <span style={{ display: "flex", alignItems: "center", gap: 14 }}>
          <button type="button" className="mu-btn mu-btn-secondary mu-btn-sm" onClick={() => setSent(null)}>
            Ask for another
          </button>
          <span className="m-note-mono">sent just now</span>
        </span>
      </div>
    );
  }

  return (
    <form onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 8 }} noValidate>
      <div style={{ display: "flex", gap: 8 }}>
        <input
          aria-label="Provider name"
          name="provider"
          value={value}
          maxLength={MAX}
          autoComplete="off"
          placeholder="Railway, Fly.io, Firebase…"
          onChange={(e) => {
            setValue(e.target.value);
            if (error) setError(null);
          }}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? "req-err" : undefined}
          style={{ flex: "1 1 auto", minWidth: 0, height: 44, boxSizing: "border-box", padding: "0 14px", border: "1px solid var(--control-line)", borderRadius: "var(--radius-md)", background: "var(--surface-sunken)", color: "var(--ink)", font: "14px var(--font-sans)" }}
        />
        <button type="submit" className="mu-btn mu-btn-secondary" disabled={pending} style={{ height: 44, padding: "0 18px", fontSize: 14 }}>
          {pending ? "Sending…" : "Request"}
        </button>
      </div>
      {error ? (
        <span id="req-err" role="alert" style={{ font: "12.5px var(--font-mono)", color: "var(--ink-muted)" }}>
          {error}
        </span>
      ) : null}
    </form>
  );
}
