import Link from "next/link";
import { AuthCard } from "@/components/auth-card";

export default function NotFound() {
  return (
    <AuthCard title="Nothing here">
      <p>This page doesn&apos;t exist.</p>
      <Link href="/" className="mt-5 inline-flex text-sm text-fg underline underline-offset-2">
        Back to Muretto
      </Link>
    </AuthCard>
  );
}
