import { useSyncExternalStore } from "react";

type SoftwareUpdate = { available: boolean; apply: () => void };

let waitingWorker: ServiceWorker | null = null;
let updateAvailable = false;
let reloadRequested = false;
const updateListeners = new Set<() => void>();

function notifyUpdateListeners() {
  for (const listener of updateListeners) listener();
}

function subscribeUpdates(listener: () => void) {
  updateListeners.add(listener);
  return () => {
    updateListeners.delete(listener);
  };
}

function applyUpdate() {
  if (!waitingWorker) return;
  reloadRequested = true;
  waitingWorker.postMessage({ type: "skip-waiting" });
}

export function useSoftwareUpdate(): SoftwareUpdate {
  const available = useSyncExternalStore(
    subscribeUpdates,
    () => updateAvailable,
    () => false,
  );
  return { available, apply: applyUpdate };
}

const onlineListeners = new Set<() => void>();

function notifyOnlineListeners() {
  for (const listener of onlineListeners) listener();
}

function subscribeOnline(listener: () => void) {
  onlineListeners.add(listener);
  return () => {
    onlineListeners.delete(listener);
  };
}

export function useOnlineStatus(): boolean {
  return useSyncExternalStore(
    subscribeOnline,
    () => navigator.onLine,
    () => true,
  );
}

export function registerServiceWorker(): void {
  if (!import.meta.env.PROD) return;
  if (!("serviceWorker" in navigator)) return;

  window.addEventListener("online", notifyOnlineListeners);
  window.addEventListener("offline", notifyOnlineListeners);
  navigator.serviceWorker.addEventListener("controllerchange", () => {
    if (reloadRequested) window.location.reload();
  });

  const start = () => {
    navigator.serviceWorker
      .register("/sw.js")
      .then((registration) => {
        if (registration.waiting && navigator.serviceWorker.controller) {
          waitingWorker = registration.waiting;
          updateAvailable = true;
          notifyUpdateListeners();
        }
        registration.addEventListener("updatefound", () => {
          const installing = registration.installing;
          if (!installing) return;
          installing.addEventListener("statechange", () => {
            if (installing.state === "installed" && navigator.serviceWorker.controller) {
              waitingWorker = installing;
              updateAvailable = true;
              notifyUpdateListeners();
            }
          });
        });
      })
      .catch(() => undefined);
  };

  if (document.readyState === "complete") start();
  else window.addEventListener("load", start);
}
