"use server";

import { revalidatePath } from "next/cache";
import { decideReview, requestProvider, signOutMachine, type Decision } from "@/lib/api";
import { requireViewer } from "@/lib/auth";

const DECISIONS: Decision[] = ["keep", "not_needed", "pending"];

/** Records Keep / Not needed / undo for one resource. Returns false if it didn't save. */
export async function decide(resourceId: string, state: Decision): Promise<boolean> {
  if (typeof resourceId !== "string" || resourceId.length > 512 || !DECISIONS.includes(state)) return false;
  const viewer = await requireViewer();
  try {
    await decideReview(viewer, resourceId, state);
  } catch {
    return false;
  }
  revalidatePath("/", "layout");
  return true;
}

/** Signs one CLI machine out (revokes its token). */
export async function signOutMachineAction(id: string): Promise<boolean> {
  if (typeof id !== "string" || id.length > 128) return false;
  const viewer = await requireViewer();
  try {
    await signOutMachine(viewer, id);
  } catch {
    return false;
  }
  revalidatePath("/settings");
  return true;
}

export async function requestProviderAction(name: string): Promise<boolean> {
  const clean = typeof name === "string" ? name.trim().slice(0, 60) : "";
  if (!clean) return false;
  const viewer = await requireViewer();
  try {
    await requestProvider(viewer, clean);
  } catch {
    return false;
  }
  return true;
}
