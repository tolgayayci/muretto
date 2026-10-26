// Typed client for the Muretto sync API (docs/ARCHITECTURE.md, "Sync API").
//
// Every call goes through /api/*, which next.config.ts proxies to the Go server,
// so the HttpOnly session cookie is same-origin. Entity types come from the
// generated contract (src/contract, from schema/*.schema.json). The few response
// shapes the schema does not define (/v1/me, invites, device approval) are
// declared here, exactly as the architecture table describes them.
import type { Answer, Org } from "@/contract";

export const API_BASE = "/api";

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
}

/** POST /v1/orgs/{id}/invites */
export interface InviteLink {
  url: string;
}

/** POST /v1/provider-requests */
export interface ProviderRequest {
  provider: string;
  email?: string;
}

/** One message on GET /v1/orgs/{id}/events. */
export interface OrgEvent {
  type: string;
  id: string;
  developer?: string;
}

export type ApiErrorKind = "unauthorized" | "forbidden" | "not_found" | "invalid" | "rate_limited" | "unreachable" | "server";

export class ApiError extends Error {
  readonly status: number;
  readonly kind: ApiErrorKind;
  readonly code?: string;

  constructor(status: number, kind: ApiErrorKind, message: string, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.kind = kind;
    this.code = code;
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

// CSRF: double-submit. If the server sets a readable `muretto_csrf` cookie, every
// POST echoes it in X-CSRF-Token (docs/research/auth-login.md asks for CSRF
// protection on device approval and invite acceptance).
export const CSRF_COOKIE = "muretto_csrf";
export const CSRF_HEADER = "X-CSRF-Token";

function csrfToken(): string | undefined {
  if (typeof document === "undefined") return undefined;
  const m = document.cookie.match(new RegExp(`(?:^|; )${CSRF_COOKIE}=([^;]*)`));
  return m ? decodeURIComponent(m[1]) : undefined;
}

async function request<T>(method: "GET" | "POST", path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  let res: Response;
  const csrf = method === "POST" ? csrfToken() : undefined;
  try {
    res = await fetch(API_BASE + path, {
      method,
      credentials: "same-origin",
      cache: "no-store",
      headers: {
        Accept: "application/json",
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
        ...(csrf ? { [CSRF_HEADER]: csrf } : {}),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
    });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") throw err;
    throw new ApiError(0, "unreachable", "Can't reach the Muretto server.");
  }

  const isJSON = (res.headers.get("content-type") ?? "").includes("application/json");
  const data: unknown = isJSON ? await res.json().catch(() => undefined) : undefined;

  if (!res.ok) {
    const obj = (data ?? {}) as { error?: unknown; message?: unknown };
    const code = typeof obj.error === "string" ? obj.error : undefined;
    const message = typeof obj.message === "string" ? obj.message : code ?? `${res.status} ${res.statusText}`;
    throw new ApiError(res.status, kindFor(res.status, isJSON), message, code);
  }
  return data as T;
}

const seg = encodeURIComponent;

export const api = {
  me: (signal?: AbortSignal) => request<Me>("GET", "/v1/me", undefined, signal),

  /** The caller's own synced data (the "Me" scope). */
  myAnswer: (signal?: AbortSignal) => request<Answer>("GET", "/v1/answer?scope=me", undefined, signal),

  /** The team view: union of the org members' data, with developer names. */
  orgAnswer: (orgId: string, signal?: AbortSignal) =>
    request<Answer>("GET", `/v1/orgs/${seg(orgId)}/answer`, undefined, signal),

  createOrg: (name: string) => request<Org>("POST", "/v1/orgs", { name }),

  createInvite: (orgId: string) => request<InviteLink>("POST", `/v1/orgs/${seg(orgId)}/invites`),

  acceptInvite: (token: string) => request<unknown>("POST", `/v1/invites/${seg(token)}/accept`),

  approveDevice: (userCode: string) => request<unknown>("POST", "/v1/device/approve", { user_code: userCode }),

  requestProvider: (req: ProviderRequest) => request<unknown>("POST", "/v1/provider-requests", req),

  logout: () => request<unknown>("POST", "/auth/logout"),

  /** Full-page navigation target that starts GitHub sign-in on the Go server. */
  signInURL: () => `${API_BASE}/auth/github/login`,

  /** SSE stream of org events (cookie auth). */
  eventsURL: (orgId: string) => `${API_BASE}/v1/orgs/${seg(orgId)}/events`,
};

// Where to land after GitHub sign-in. The OAuth callback is the server's, so the
// dashboard remembers the page itself (per tab) and resumes it from /app.
const RETURN_KEY = "muretto.returnTo";

export function rememberReturnTo(path: string) {
  try {
    sessionStorage.setItem(RETURN_KEY, path);
  } catch {
    /* storage unavailable: the user lands on /app instead */
  }
}

export function takeReturnTo(): string | null {
  try {
    const v = sessionStorage.getItem(RETURN_KEY);
    sessionStorage.removeItem(RETURN_KEY);
    return v && v.startsWith("/") && !v.startsWith("//") ? v : null;
  } catch {
    return null;
  }
}
