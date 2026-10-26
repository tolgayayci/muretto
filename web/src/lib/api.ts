// Typed client for the Muretto sync API: docs/ARCHITECTURE.md ("Sync API") plus
// the dashboard notes in server/README.md.
//
// Base URL: NEXT_PUBLIC_MURETTO_API_BASE, default "/api". In development /api/*
// is proxied by next.config.ts to the Go server (MURETTO_API_URL), so the session
// cookie is same-origin. In production, point it at the API's own origin on a
// sibling subdomain (https://api.<domain>); the server allows the dashboard
// origin with credentials, and the SameSite=Lax cookie is sent because both are
// same-site.
//
// Entity types come from the generated contract (src/contract). The few response
// shapes the schema does not define (/v1/me, invites, device) are declared here.
import type { Answer, Org } from "@/contract";

export const API_BASE = (process.env.NEXT_PUBLIC_MURETTO_API_BASE ?? "/api").replace(/\/+$/, "");

/** A team the signed-in user belongs to, as listed by GET /v1/me. */
export interface OrgRef {
  id: string;
  name: string;
  role: "owner" | "member";
}

/** GET /v1/me */
export interface Me {
  login: string;
  display_name: string;
  orgs: OrgRef[];
  /** Present for cookie auth: echoed in X-CSRF-Token on POST/DELETE. */
  csrf_token?: string;
}

/** POST /v1/orgs/{id}/invites */
export interface InviteLink {
  url: string;
  expires_at?: string;
}

/** GET /v1/device?user_code=… */
export interface DeviceInfo {
  user_code: string;
  client?: { client?: string; host?: string; os?: string; version?: string };
  expires_at?: string;
}

/** POST /v1/device/approve */
export interface DeviceDecision {
  status: "approved" | "denied";
}

/** POST /v1/provider-requests */
export interface ProviderRequest {
  provider: string;
  email?: string;
}

/**
 * One message on GET /v1/orgs/{id}/events. Ids only: the dashboard refetches the
 * answer. type "bulk" means one upload changed more than 100 entities.
 */
export interface OrgEvent {
  seq?: number;
  type: string;
  id: string;
  developer?: string;
  at?: string;
}

export type ApiErrorKind = "unauthorized" | "forbidden" | "not_found" | "invalid" | "rate_limited" | "unreachable" | "server";

export class ApiError extends Error {
  readonly status: number;
  readonly kind: ApiErrorKind;
  readonly code?: string;
  readonly requestId?: string;

  constructor(status: number, kind: ApiErrorKind, message: string, code?: string, requestId?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.kind = kind;
    this.code = code;
    this.requestId = requestId;
  }
}

function kindFor(status: number, isJSON: boolean): ApiErrorKind {
  if (status === 401) return "unauthorized";
  if (status === 403) return "forbidden";
  if (status === 404) return "not_found";
  if (status === 400 || status === 409 || status === 410 || status === 422) return "invalid";
  if (status === 429) return "rate_limited";
  // The dev proxy answers 500/502/504 with an HTML page when the Go server is down.
  if (status === 502 || status === 503 || status === 504 || (status === 500 && !isJSON)) return "unreachable";
  return "server";
}

export const CSRF_HEADER = "X-CSRF-Token";

// The CSRF token comes from GET /v1/me. Cached per page load.
let csrf: string | undefined;

type Method = "GET" | "POST" | "DELETE";

async function request<T>(
  method: Method,
  path: string,
  opts: { body?: unknown; signal?: AbortSignal; csrf?: boolean } = {},
): Promise<T> {
  if (opts.csrf && !csrf) {
    // Fetch the token first. If not signed in, the real request returns 401 below.
    await me(opts.signal).catch((e: unknown) => {
      if (e instanceof ApiError && e.kind === "unauthorized") return;
      throw e;
    });
  }
  let res: Response;
  try {
    res = await fetch(API_BASE + path, {
      method,
      credentials: "include",
      cache: "no-store",
      headers: {
        Accept: "application/json",
        ...(opts.body === undefined ? {} : { "Content-Type": "application/json" }),
        ...(opts.csrf && csrf ? { [CSRF_HEADER]: csrf } : {}),
      },
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
      signal: opts.signal,
    });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") throw err;
    throw new ApiError(0, "unreachable", "Can't reach the Muretto server.");
  }

  const isJSON = (res.headers.get("content-type") ?? "").includes("application/json");
  const data: unknown = isJSON && res.status !== 204 ? await res.json().catch(() => undefined) : undefined;

  if (!res.ok) {
    const obj = (data ?? {}) as { error?: unknown; message?: unknown; request_id?: unknown };
    const code = typeof obj.error === "string" ? obj.error : undefined;
    const message = typeof obj.message === "string" ? obj.message : (code ?? `${res.status} ${res.statusText}`);
    if (res.status === 401) csrf = undefined;
    throw new ApiError(res.status, kindFor(res.status, isJSON), message, code, typeof obj.request_id === "string" ? obj.request_id : undefined);
  }
  return data as T;
}

async function me(signal?: AbortSignal): Promise<Me> {
  const m = await request<Me>("GET", "/v1/me", { signal });
  csrf = m.csrf_token;
  return m;
}

const seg = encodeURIComponent;

export const api = {
  me,

  /** The caller's own synced data (the "Me" scope). */
  myAnswer: (signal?: AbortSignal) => request<Answer>("GET", "/v1/answer?scope=me", { signal }),

  /** The team view: union of the org members' data, with developer names. */
  orgAnswer: (orgId: string, signal?: AbortSignal) => request<Answer>("GET", `/v1/orgs/${seg(orgId)}/answer`, { signal }),

  org: (orgId: string, signal?: AbortSignal) => request<Org>("GET", `/v1/orgs/${seg(orgId)}`, { signal }),

  createOrg: (name: string) => request<Org>("POST", "/v1/orgs", { body: { name }, csrf: true }),

  createInvite: (orgId: string) => request<InviteLink>("POST", `/v1/orgs/${seg(orgId)}/invites`, { csrf: true }),

  acceptInvite: (token: string) => request<OrgRef>("POST", `/v1/invites/${seg(token)}/accept`, { csrf: true }),

  device: (userCode: string, signal?: AbortSignal) =>
    request<DeviceInfo>("GET", `/v1/device?user_code=${seg(userCode)}`, { signal }),

  decideDevice: (userCode: string, deny = false) =>
    request<DeviceDecision>("POST", "/v1/device/approve", {
      body: deny ? { user_code: userCode, deny: true } : { user_code: userCode },
      csrf: true,
    }),

  /** No auth: rate-limited per IP. */
  requestProvider: (req: ProviderRequest) => request<unknown>("POST", "/v1/provider-requests", { body: req }),

  logout: () => request<unknown>("POST", "/auth/logout", { csrf: true }),

  /** Full-page navigation that starts GitHub sign-in; the server returns to `returnTo` (a relative path). */
  signInURL: (returnTo = "/app") =>
    `${API_BASE}/auth/github/login?return_to=${seg(safePath(returnTo))}`,

  /** Dev only: the server's MURETTO_DEV_LOGIN sign-in (loopback servers only). */
  devSignInURL: (login: string, returnTo = "/app") =>
    `${API_BASE}/auth/dev/login?login=${seg(login)}&name=${seg(login)}&return_to=${seg(safePath(returnTo))}`,

  /** SSE stream of org events (cookie auth). */
  eventsURL: (orgId: string) => `${API_BASE}/v1/orgs/${seg(orgId)}/events`,
};

/** Only same-site relative paths may be used as a post-login destination. */
export function safePath(p: string | null | undefined): string {
  return p && p.startsWith("/") && !p.startsWith("//") && !p.startsWith("/\\") ? p : "/app";
}
