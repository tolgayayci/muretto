import Link from "next/link";
import type { ReactNode } from "react";
import { Wordmark } from "./ui";

/** Centered card used by sign-in, device approval, team creation and invites. */
export function AuthCard({ title, children, footer }: { title: string; children: ReactNode; footer?: ReactNode }) {
  return (
    <div className="grid-bg flex min-h-dvh flex-col items-center px-4 pt-[14vh] pb-10">
      <Link href="/" className="mb-8 rounded-md" aria-label="Muretto home">
        <Wordmark />
      </Link>
      <main className="w-full max-w-[420px] rounded-2xl border border-line bg-panel p-7 shadow-panel">
        <h1 className="text-lg font-semibold tracking-[-0.01em]">{title}</h1>
        <div className="mt-2 text-sm text-muted">{children}</div>
      </main>
      {footer ? <div className="mt-6 max-w-[420px] text-center text-xs text-faint">{footer}</div> : null}
    </div>
  );
}

export const buttonPrimary =
  "inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-accent px-4 text-sm font-medium text-accent-ink transition-colors hover:bg-accent-strong disabled:cursor-not-allowed disabled:opacity-60";

export const inputClass =
  "h-10 w-full rounded-lg border border-line-strong bg-sunken px-3 text-sm text-fg placeholder:text-faint focus-visible:border-accent";
