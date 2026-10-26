import type { Metadata } from "next";
import { Suspense } from "react";
import { AuthCard } from "@/components/auth-card";
import { DeviceApproval } from "./approve";

export const metadata: Metadata = { title: "Approve CLI login" };

export default function DevicePage() {
  return (
    <AuthCard
      title="Approve a CLI login"
      footer="Only approve a code you started yourself with muretto login. The CLI gets a token that can sync your data; it can never change your infrastructure."
    >
      <Suspense fallback={<div className="h-[148px]" />}>
        <DeviceApproval />
      </Suspense>
    </AuthCard>
  );
}
