"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import type { Answer } from "@/contract";
import { api, ApiError, takeReturnTo, type Me } from "@/lib/api";
import { useOrgEvents } from "@/lib/events";
import { ClockProvider } from "../clock";
import { Board, type Scope } from "./board";
import { DashboardHeader, LiveIndicator } from "./header";

const SCOPE_KEY = "muretto.scope";
const ORG_KEY = "muretto.org";

function read(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}
function write(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* storage unavailable: the choice just isn't remembered */
  }
}

type Phase = "loading" | "ready" | "unreachable" | "error";

/** The dashboard backed by the sync API, with live updates over SSE. */
export function LiveDashboard() {
  const router = useRouter();
  const [phase, setPhase] = useState<Phase>("loading");
  const [problem, setProblem] = useState<string | null>(null);
  const [me, setMe] = useState<Me | null>(null);
  const [scope, setScope] = useState<Scope>("team");
  const [orgId, setOrgId] = useState<string | null>(null);
  const [data, setData] = useState<{ key: string; answer: Answer } | null>(null);

  const key = `${scope}|${scope === "team" ? orgId : ""}`;

  // Who am I, and which teams can I see?
  useEffect(() => {
    const ac = new AbortController();
    (async () => {
      const back = takeReturnTo();
      if (back && back !== "/app") {
        router.replace(back);
        return;
      }
      try {
        const m = await api.me(ac.signal);
        const stored = read(ORG_KEY);
        const org = m.orgs.find((o) => o.id === stored) ?? m.orgs[0];
        setMe(m);
        setOrgId(org?.id ?? null);
        setScope(org && read(SCOPE_KEY) !== "me" ? "team" : "me");
        setPhase("ready");
      } catch (e) {
        if (ac.signal.aborted) return;
        if (e instanceof ApiError && e.kind === "unauthorized") {
          router.replace("/signin");
        } else if (e instanceof ApiError && e.kind === "unreachable") {
          setPhase("unreachable");
        } else {
          setProblem(e instanceof Error ? e.message : "Something went wrong.");
          setPhase("error");
        }
      }
    })();
    return () => ac.abort();
  }, [router]);

  const load = useCallback(
    (signal?: AbortSignal) => {
      if (!me) return;
      const k = `${scope}|${scope === "team" ? orgId : ""}`;
      const request = scope === "team" && orgId ? api.orgAnswer(orgId, signal) : api.myAnswer(signal);
      request.then(
        (answer) => {
          setData({ key: k, answer });
          setProblem(null);
        },
        (e: unknown) => {
          if (signal?.aborted) return;
          if (e instanceof ApiError && e.kind === "unauthorized") router.replace("/signin");
          else
            setProblem(
              e instanceof ApiError && e.kind === "unreachable" ? "Lost contact with the server. Retrying." : (e as Error).message,
            );
        },
      );
    },
    [me, scope, orgId, router],
  );

  useEffect(() => {
    const ac = new AbortController();
    load(ac.signal);
    return () => ac.abort();
  }, [load]);

  // Live: any org event triggers one debounced refetch through the normal read API.
  const debounce = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const onEvent = useCallback(() => {
    clearTimeout(debounce.current);
    debounce.current = setTimeout(() => load(), 300);
  }, [load]);
  useEffect(() => () => clearTimeout(debounce.current), []);
  const live = useOrgEvents(phase === "ready" ? orgId : null, onEvent);

  // Without a team there is no event stream: poll gently instead.
  useEffect(() => {
    if (phase !== "ready" || orgId) return;
    const t = setInterval(() => load(), 30_000);
    return () => clearInterval(t);
  }, [phase, orgId, load]);

  const changeScope = useCallback(
    (s: Scope) => {
      if (s === "team" && !orgId) return;
      setScope(s);
      write(SCOPE_KEY, s);
    },
    [orgId],
  );
  const changeOrg = (id: string) => {
    setOrgId(id);
    write(ORG_KEY, id);
  };

  const signOut = async () => {
    try {
      await api.logout();
    } finally {
      router.replace("/");
    }
  };

  const status = orgId ? (
    <LiveIndicator status={live} />
  ) : (
    <span className="text-xs text-muted">Updates every 30 s</span>
  );

  return (
    <ClockProvider start={0} pinned={false}>
      <div className="min-h-dvh">
        <DashboardHeader
          scope={scope}
          onScope={changeScope}
          orgs={me?.orgs ?? []}
          orgId={orgId}
          onOrg={changeOrg}
          sample={false}
          status={phase === "ready" ? status : null}
          user={me?.login}
          onSignOut={me ? signOut : undefined}
        />
        <main className="mx-auto max-w-[1600px] px-4 py-5 sm:px-6">
          {problem && phase === "ready" ? (
            <p role="alert" className="mb-4 rounded-lg border border-line bg-panel px-4 py-2 text-sm text-muted">
              {problem}
            </p>
          ) : null}
          {phase === "unreachable" ? (
            <Unreachable />
          ) : phase === "error" ? (
            <Problem message={problem ?? "Something went wrong."} />
          ) : data && data.key === key ? (
            <Board answer={data.answer} scope={scope} sample={false} feedStatus={status} onScope={changeScope} />
          ) : (
            <Skeleton />
          )}
        </main>
      </div>
    </ClockProvider>
  );
}

function Unreachable() {
  return (
    <section className="mx-auto mt-16 max-w-lg rounded-2xl border border-line bg-panel p-8 shadow-panel">
      <h1 className="text-lg font-semibold">Can&apos;t reach the Muretto server</h1>
      <p className="mt-2 text-sm text-muted">
        The dashboard talks to the sync API through <code className="font-mono text-fg">/api</code>. In development, start the Go
        server or set <code className="font-mono text-fg">MURETTO_API_URL</code>.
      </p>
      <Link
        href="/app/sample"
        className="mt-5 inline-flex h-9 items-center rounded-lg border border-line-strong px-4 text-sm font-medium hover:bg-hover"
      >
        View the dashboard with sample data
      </Link>
    </section>
  );
}

function Problem({ message }: { message: string }) {
  return (
    <section role="alert" className="mx-auto mt-16 max-w-lg rounded-2xl border border-line bg-panel p-8">
      <h1 className="text-lg font-semibold">The dashboard couldn&apos;t load</h1>
      <p className="mt-2 font-mono text-sm text-muted">{message}</p>
    </section>
  );
}

/** Same footprint as the loaded board, so nothing jumps when data arrives. */
export function Skeleton() {
  const block = "rounded-xl border border-line bg-panel";
  return (
    <div aria-busy="true" aria-label="Loading" className="flex animate-pulse flex-col gap-4">
      <div className={`${block} h-[72px]`} />
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
        <div className="flex flex-col gap-4">
          <div className={`${block} h-[520px]`} />
          <div className={`${block} h-[220px]`} />
        </div>
        <div className="flex flex-col gap-4">
          <div className={`${block} h-[260px]`} />
          <div className={`${block} h-[376px]`} />
        </div>
      </div>
    </div>
  );
}
