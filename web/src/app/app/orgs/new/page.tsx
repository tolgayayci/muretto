import type { Metadata } from "next";
import { AuthCard } from "@/components/auth-card";
import { CreateOrgForm } from "./form";

export const metadata: Metadata = { title: "Create a team" };

export default function NewOrgPage() {
  return (
    <AuthCard
      title="Create a team"
      footer="A team view is the union of its members' synced data. Each person's data stays theirs."
    >
      <CreateOrgForm />
    </AuthCard>
  );
}
