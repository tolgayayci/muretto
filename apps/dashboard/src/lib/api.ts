import "server-only";
import { redirect } from "next/navigation";
import type { Answer } from "@/contract";
import { API_URL } from "@/lib/config";
import { buildModel, type Model } from "@/lib/model";
import { SAMPLE_NOW, sampleAnswer } from "@/lib/sample";
import type { Viewer } from "@/lib/auth";

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

/** Calls the sync API as the viewer. Never call this from client code. */
async function call<T>(viewer: Viewer, path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  if (viewer.kind === "sample") throw new ApiError(400, "sample", "not available with sample data");
  let res: Response;
  try {
    res = await fetch(`${API_URL}${path}`, {
      method: init.method ?? "GET",
      headers: {
        Authorization: `Bearer ${viewer.token}`,
        Accept: "application/json",
        ...(init.body !== undefined ? { "Content-Type": "application/json" } : {}),
      },
      body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
      cache: "no-store",
      redirect: "error",
      signal: AbortSignal.timeout(10_000),
    });
  } catch {
    throw new ApiError(503, "unreachable", "Muretto's API didn't answer");
  }
  if (res.status === 401) redirect("/auth/signout?reason=expired");
  if (!res.ok) {
    let code = "error";
    let message = res.statusText;
    try {
      const body = (await res.json()) as { error?: { code?: string; message?: string } };
      code = body.error?.code ?? code;
      message = body.error?.message ?? message;
    } catch {
      // not JSON
    }
    throw new ApiError(res.status, code, message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export async function getAnswer(viewer: Viewer): Promise<Answer> {
  if (viewer.kind === "sample") return sampleAnswer(viewer.scenario);
  return call<Answer>(viewer, "/v1/answer");
}

/** The Answer turned into everything the pages show. */
export async function getModel(viewer: Viewer): Promise<Model> {
  const answer = await getAnswer(viewer);
  return buildModel(answer, viewer.kind === "sample" ? SAMPLE_NOW : new Date());
}

export interface Machine {
  id: string;
  name: string;
  created_at: string;
  last_used_at?: string;
}

function sampleMachines(scenario: string): Machine[] {
  if (scenario === "new") return [];
  // The Mac mini goes quiet only in the "problem" sample.
  const miniLastUsed = scenario === "problem" ? "2026-10-30T14:30:00Z" : "2026-11-01T09:04:00Z";
  return [
    { id: "tok_sample_mbp", name: "alice's MacBook Pro", created_at: "2026-10-20T09:00:00Z", last_used_at: "2026-11-01T14:39:55Z" },
    { id: "tok_sample_mini", name: "alice's Mac mini", created_at: "2026-10-22T18:00:00Z", last_used_at: miniLastUsed },
  ];
}

export async function getMachines(viewer: Viewer): Promise<Machine[]> {
  if (viewer.kind === "sample") return sampleMachines(viewer.scenario);
  const body = await call<{ tokens: Machine[] }>(viewer, "/v1/tokens");
  return body.tokens ?? [];
}

export async function signOutMachine(viewer: Viewer, id: string): Promise<void> {
  if (viewer.kind === "sample") return;
  await call<void>(viewer, `/v1/tokens/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export interface DeviceInfo {
  user_code: string;
  client: { client?: string; host?: string; os?: string; version?: string };
  expires_at: string;
}

export async function lookupDevice(viewer: Viewer, userCode: string): Promise<DeviceInfo | null> {
  if (viewer.kind === "sample") return { user_code: userCode, client: { client: "muretto", host: "alice's MacBook Pro", os: "macOS · arm64", version: "0.4.0" }, expires_at: new Date(Date.now() + 600_000).toISOString() };
  try {
    return await call<DeviceInfo>(viewer, `/v1/device?user_code=${encodeURIComponent(userCode)}`);
  } catch (e) {
    if (e instanceof ApiError && (e.status === 404 || e.status === 400)) return null;
    throw e;
  }
}

export async function decideDevice(viewer: Viewer, userCode: string, approve: boolean): Promise<"approved" | "denied"> {
  if (viewer.kind === "sample") return approve ? "approved" : "denied";
  const body = await call<{ status: string }>(viewer, "/v1/device/approve", { method: "POST", body: { user_code: userCode, deny: !approve } });
  return body.status === "approved" ? "approved" : "denied";
}

export type Decision = "keep" | "not_needed" | "pending";

export async function decideReview(viewer: Viewer, resourceId: string, state: Decision): Promise<void> {
  if (viewer.kind === "sample") return;
  await call<void>(viewer, "/v1/reviews", { method: "POST", body: { resource_id: resourceId, state } });
}

export async function requestProvider(viewer: Viewer, provider: string): Promise<void> {
  if (viewer.kind === "sample") return;
  await call<void>(viewer, "/v1/provider-requests", { method: "POST", body: { provider } });
}
