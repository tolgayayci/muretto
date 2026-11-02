import type { Metadata } from "next";
import { loadModel } from "@/lib/data";
import { Connected, NothingConnected } from "./_components/view";

export const metadata: Metadata = { title: "Providers" };

export default async function ProvidersPage() {
  const m = await loadModel();
  if (!m.hasProviders) return <NothingConnected />;
  return <Connected m={m} />;
}
