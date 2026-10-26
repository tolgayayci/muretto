"use client";

import { useEffect, useRef, useState } from "react";
import { api, type OrgEvent } from "./api";

export type LiveStatus = "off" | "connecting" | "live" | "reconnecting";

/**
 * Subscribes to GET /v1/orgs/{id}/events and calls onEvent for every event.
 *
 * The server sends default `message` events with `id: <seq>` and JSON data
 * {seq, type, id, developer, at}, plus a heartbeat every 20 s. EventSource
 * retries by itself after a dropped connection (sending Last-Event-ID, which the
 * server replays). If the browser gives up (readyState CLOSED, e.g. a 404 for a
 * non-member or a 5xx), we reopen it with exponential backoff, capped at 30 s.
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

    const open = () => {
      if (stopped) return;
      setStatus(attempt === 0 ? "connecting" : "reconnecting");
      es = new EventSource(api.eventsURL(orgId), { withCredentials: true });
      es.onopen = () => {
        attempt = 0;
        setStatus("live");
      };
      es.onmessage = (msg: MessageEvent<string>) => {
        let data: Partial<OrgEvent> = {};
        try {
          data = JSON.parse(msg.data) as Partial<OrgEvent>;
        } catch {
          // Not JSON: still a signal that something changed.
        }
        handler.current({
          seq: typeof data.seq === "number" ? data.seq : undefined,
          type: typeof data.type === "string" ? data.type : "message",
          id: typeof data.id === "string" ? data.id : msg.lastEventId,
          developer: typeof data.developer === "string" ? data.developer : undefined,
          at: typeof data.at === "string" ? data.at : undefined,
        });
      };
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

    // Open from a timer so no state is set synchronously in the effect body.
    retry = setTimeout(open, 0);
    return () => {
      stopped = true;
      clearTimeout(retry);
      es?.close();
    };
  }, [orgId]);

  return orgId ? status : "off";
}
