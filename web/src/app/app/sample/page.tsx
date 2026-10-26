import type { Metadata } from "next";
import { SampleDashboard } from "@/components/dashboard/sample";

export const metadata: Metadata = {
  title: "Sample dashboard",
  description: "The Muretto dashboard rendered from built-in sample data (acme, alice, bob…).",
};

export default function SampleDashboardPage() {
  return <SampleDashboard />;
}
