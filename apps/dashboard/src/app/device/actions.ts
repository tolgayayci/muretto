"use server";

import { redirect } from "next/navigation";
import { ApiError, decideDevice } from "@/lib/api";
import { requireViewer } from "@/lib/auth";
import { normalizeCode } from "@/lib/device-code";

/**
 * Approves or denies a CLI login. Server Actions only accept same-origin POSTs,
 * and the API checks again that this is a signed-in dashboard user (a CLI token
 * can't approve devices).
 */
export async function decideDeviceAction(form: FormData): Promise<void> {
  const code = normalizeCode(String(form.get("code") ?? ""));
  const approve = form.get("decision") === "approve";
  if (!code) redirect("/device?result=invalid");
  const viewer = await requireViewer();
  let result: string;
  try {
    result = await decideDevice(viewer, code, approve);
  } catch (e) {
    result = e instanceof ApiError && (e.status === 404 || e.status === 409 || e.status === 410) ? "expired" : "error";
  }
  redirect(`/device?result=${result}`);
}

/** The "type the code" form: normalizes and moves the code into the URL. */
export async function enterCodeAction(form: FormData): Promise<void> {
  const code = normalizeCode(String(form.get("code") ?? ""));
  redirect(code ? `/device?code=${code}` : "/device?result=invalid");
}
