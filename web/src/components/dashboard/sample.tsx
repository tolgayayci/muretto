"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { buildView } from "@/lib/model";
import { SAMPLE_NOW, SAMPLE_ORG, sampleMe, sampleMeAnswer, sampleTeamAnswer } from "@/lib/sample";
import { ClockProvider } from "../clock";
import { Board, type Scope } from "./board";
import { DashboardHeader } from "./header";

const REPLAY = 3;

/**
 * Replays the newest few feed items one by one, the way they would arrive from
 * agents, once the board is on screen. Only the feed replays; it is sample data.
 */
function useReplay(keys: string[], target: React.RefObject<HTMLElement | null>) {
  const newest = keys.slice(0, REPLAY);
  const [shown, setShown] = useState(0);
  const newestKey = newest.join("|");

  useEffect(() => {
    const el = target.current;
    let timer: ReturnType<typeof setInterval> | undefined;
    let started = false;
    const start = () => {
      if (started) return;
      started = true;
      timer = setInterval(() => {
        setShown((n) => {
          if (n + 1 >= REPLAY) clearInterval(timer);
          return Math.min(REPLAY, n + 1);
        });
      }, 2600);
    };
    if (!el || typeof IntersectionObserver === "undefined") {
      start();
      return () => clearInterval(timer);
    }
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        start();
        io.disconnect();
      }
    });
    io.observe(el);
    return () => {
      io.disconnect();
      clearInterval(timer);
    };
  }, [newestKey, target]);

  // Hide the newest items not yet "arrived" (oldest of them arrives first).
  return useMemo(() => new Set(newest.slice(0, REPLAY - shown)), [newestKey, shown]); // eslint-disable-line react-hooks/exhaustive-deps
}

/** The dashboard rendered from built-in sample data. Clearly labelled. */
export function SampleDashboard() {
  const [scope, setScope] = useState<Scope>("team");
  const answer = scope === "team" ? sampleTeamAnswer : sampleMeAnswer;
  const ref = useRef<HTMLElement>(null);
  const keys = useMemo(() => buildView(sampleTeamAnswer).feed.map((f) => f.key), []);
  const hidden = useReplay(keys, ref);

  const status = <span className="text-xs text-muted">Replaying sample events</span>;

  return (
    <ClockProvider start={Date.parse(SAMPLE_NOW)} pinned>
      <div className="min-h-dvh">
        <DashboardHeader
          scope={scope}
          onScope={setScope}
          orgs={sampleMe.orgs}
          orgId={SAMPLE_ORG.id}
          onOrg={() => {}}
          sample
          status={status}
          user={sampleMe.login}
        />
        <main ref={ref} className="mx-auto max-w-[1600px] px-4 py-5 sm:px-6">
          <Board answer={answer} scope={scope} sample feedStatus={status} hiddenFeed={hidden} onScope={setScope} />
        </main>
      </div>
    </ClockProvider>
  );
}

/** A non-interactive, scaled preview of the sample dashboard for the landing page. */
export function SamplePreview() {
  const ref = useRef<HTMLDivElement>(null);
  const keys = useMemo(() => buildView(sampleTeamAnswer).feed.map((f) => f.key), []);
  const hidden = useReplay(keys, ref);
  const status = <span className="text-xs text-muted">Replaying sample events</span>;
  return (
    <ClockProvider start={Date.parse(SAMPLE_NOW)} pinned>
      <div ref={ref} inert>
        <DashboardHeader
          scope="team"
          onScope={() => {}}
          orgs={sampleMe.orgs}
          orgId={SAMPLE_ORG.id}
          onOrg={() => {}}
          sample
          status={status}
          user={sampleMe.login}
        />
        <div className="px-6 py-5">
          <Board answer={sampleTeamAnswer} scope="team" sample feedStatus={status} hiddenFeed={hidden} keyboard={false} />
        </div>
      </div>
    </ClockProvider>
  );
}
