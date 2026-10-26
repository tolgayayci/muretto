"use client";

import { useEffect, useRef, useState } from "react";
import { api, type OrgEvent } from "./api";

export type LiveStatus = "off" | "connecting" | "live" | "reconnecting";

// Named SSE event types the server may use. Unnamed events arrive on onmessage.
const NAMED = ["session", "resource", "finding", "attribution", "cost", "review", "check"];

/**
 * Subscribes to GET /v1/orgs/{id}/events and calls onEvent for every event.
 *
 * EventSource retries by itself after a dropped connection (sending
 * Last-Event-ID). If the browser gives up (readyState CLOSED, e.g. after a
 * 5xx), we reopen it with exponential backoff, capped at 30 s.
 */
export function useOrgEvents(orgId: string | null, onEvent: (e: OrgEvent) => void): LiveStatus {
  const [status, setStatus] = useState<LiveStatus>("off");
  const handler = useRef(onEvent);

  useEffect(() => {
    handler.current = onEvent;
  }, [onEvent]);

  useEffect(() => {
    if (!orgId || typeof EventSource === "undefined") return;

    let es: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;
    let stopped = false;

    const deliver = (msg: MessageEvent<string>) => {
      try {
        const data = JSON.parse(msg.data) as Partial<OrgEvent>;
        handler.current({
          type: typeof data.type === "string" ? data.type : msg.type,
          id: typeof data.id === "string" ? data.id : (msg.lastEventId ?? ""),
          developer: typeof data.developer === "string" ? data.developer : undefined,
        });
      } catch {
        // Not JSON: still a signal that something changed.
        handler.current({ type: msg.type, id: msg.lastEventId ?? "" });
      }
    };

    const open = () => {
      if (stopped) return;
      setStatus(attempt === 0 ? "connecting" : "reconnecting");
      es = new EventSource(api.eventsURL(orgId), { withCredentials: true });
      es.onopen = () => {
        attempt = 0;
        setStatus("live");
      };
      es.onmessage = deliver;
      for (const name of NAMED) es.addEventListener(name, deliver as EventListener);
      es.onerror = () => {
        if (!es || es.readyState === EventSource.CONNECTING) {
          setStatus("reconnecting");
          return;
        }
        // CLOSED: the browser will not retry. Back off and reopen.
        es.close();
        attempt += 1;
        setStatus("reconnecting");
        const delay = Math.min(30_000, 1000 * 2 ** Math.min(attempt, 5)) * (0.75 + Math.random() * 0.5);
        retry = setTimeout(open, delay);
      };
    };

    open();
    return () => {
      stopped = true;
      clearTimeout(retry);
      es?.close();
      setStatus("off");
    };
  }, [orgId]);

  return orgId ? status : "off";
}
