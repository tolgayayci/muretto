"use client";

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

export interface Clock {
  now: number;
  timeZone: string;
}

const ClockContext = createContext<Clock>({ now: 0, timeZone: "UTC" });

/**
 * Provides "now" and the display time zone. The first render uses the given
 * start time and UTC, so prerendered HTML and hydration agree. After mount:
 * - pinned (sample data): the clock runs forward from `start` in UTC;
 * - otherwise: real time, in the viewer's own time zone.
 */
export function ClockProvider({ start, pinned, children }: { start: number; pinned: boolean; children: ReactNode }) {
  const [clock, setClock] = useState<Clock>({ now: start, timeZone: "UTC" });

  useEffect(() => {
    const mountedAt = Date.now();
    const zone = pinned ? "UTC" : Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
    const tick = () => setClock({ now: pinned ? start + (Date.now() - mountedAt) : Date.now(), timeZone: zone });
    const first = setTimeout(tick, 0);
    const every = setInterval(tick, 15_000);
    return () => {
      clearTimeout(first);
      clearInterval(every);
    };
  }, [start, pinned]);

  return <ClockContext.Provider value={clock}>{children}</ClockContext.Provider>;
}

export function useClock(): Clock {
  return useContext(ClockContext);
}
