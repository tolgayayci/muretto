import type { Metadata } from "next";
import { LiveDashboard } from "@/components/dashboard/live";
import { SampleDashboard } from "@/components/dashboard/sample";

export const metadata: Metadata = { title: "Dashboard" };

export default function DashboardPage() {
  // NEXT_PUBLIC_MURETTO_SAMPLE=1 renders sample data everywhere (no server needed).
  return process.env.NEXT_PUBLIC_MURETTO_SAMPLE === "1" ? <SampleDashboard /> : <LiveDashboard />;
}
