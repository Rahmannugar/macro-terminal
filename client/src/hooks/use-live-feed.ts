import { useEffect, useRef } from "react";
import { apiURL } from "../lib/api";

const coalesceWindow = 2_500;

// useLiveFeed subscribes to the server-sent feed stream and invokes onSignal
// each time the server reports newly stored articles. Bursts of signals
// within the coalesce window are delivered as a single call so the screen
// runs one refresh instead of one per stored batch. The browser reconnects
// on its own and resumes from the last delivered event.
export function useLiveFeed(onSignal: () => void, enabled = true) {
  const signalRef = useRef(onSignal);
  signalRef.current = onSignal;
  const timerRef = useRef<number | undefined>(undefined);

  useEffect(() => {
    if (!enabled) return;
    const source = new EventSource(apiURL("/api/v1/events"), { withCredentials: true });
    const coalesce = () => {
      window.clearTimeout(timerRef.current);
      timerRef.current = window.setTimeout(() => signalRef.current(), coalesceWindow);
    };
    source.addEventListener("feed", coalesce);
    return () => {
      window.clearTimeout(timerRef.current);
      source.close();
    };
  }, [enabled]);
}
