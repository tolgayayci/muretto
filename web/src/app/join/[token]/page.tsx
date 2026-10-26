import type { Metadata } from "next";
import { Suspense } from "react";
import { AuthCard } from "@/components/auth-card";
import { AcceptInvite } from "./accept";

export const metadata: Metadata = {
  title: "Join a team",
  // Invite URLs carry a secret token: keep them out of search engines and referrers.
  robots: { index: false, follow: false },
  referrer: "no-referrer",
};

export default function JoinPage({ params }: { params: Promise<{ token: string }> }) {
  return (
    <AuthCard title="Join a team on Muretto" footer="Invite links work once and expire.">
      <Suspense fallback={<div className="h-[96px]" />}>
        <Token params={params} />
      </Suspense>
    </AuthCard>
  );
}

async function Token({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return <AcceptInvite token={token} />;
}
