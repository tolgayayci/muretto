import type { Metadata } from "next";
import Link from "next/link";
import { AuthCard, buttonPrimary } from "@/components/auth-card";
import { Icon } from "@/components/ui";
import { api } from "@/lib/api";

export const metadata: Metadata = { title: "Sign in" };

export default function SignInPage() {
  return (
    <AuthCard
      title="Sign in to Muretto"
      footer={
        <>
          Signing in is optional. The CLI works without an account.{" "}
          <Link href="/app/sample" className="underline underline-offset-2 hover:text-fg">
            See the dashboard with sample data
          </Link>
          .
        </>
      }
    >
      <p>See what your team&apos;s agent sessions created, live. Muretto uses GitHub only to know who you are.</p>
      {/* A plain link: the Go server runs the OAuth redirect and sets the session cookie. */}
      <a href={api.signInURL()} className={`${buttonPrimary} mt-6`}>
        <Icon name="github" /> Continue with GitHub
      </a>
      <ul className="mt-6 flex flex-col gap-2 text-xs text-faint">
        <li className="flex gap-2">
          <Icon name="check" className="size-3.5 shrink-0 translate-y-0.5 text-accent" />
          Muretto reads your GitHub login, then discards the GitHub token.
        </li>
        <li className="flex gap-2">
          <Icon name="check" className="size-3.5 shrink-0 translate-y-0.5 text-accent" />
          No access to your repositories or code.
        </li>
      </ul>
    </AuthCard>
  );
}
