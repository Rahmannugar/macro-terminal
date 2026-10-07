import { useOnlineStatus, useSoftwareUpdate } from "../lib/pwa";

export function PwaBanners() {
  const online = useOnlineStatus();
  const update = useSoftwareUpdate();

  if (online && !update.available) return null;

  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-0 z-[60] flex flex-col items-center gap-2 p-4">
      {!online ? (
        <p
          role="status"
          className="pointer-events-auto rounded-full bg-foreground px-4 py-2 text-xs font-medium text-background shadow-lg"
        >
          You're offline. The app stays available; live data needs a connection.
        </p>
      ) : null}
      {update.available ? (
        <div className="pointer-events-auto flex items-center gap-3 rounded-full border border-border bg-card px-4 py-2 shadow-lg">
          <p className="text-xs font-medium">A new version of Macro Terminal is ready.</p>
          <button
            type="button"
            className="rounded-full bg-primary px-3 py-1 text-xs font-semibold text-white transition outline-none hover:bg-[#076fd2] focus-visible:ring-4 focus-visible:ring-primary/20"
            onClick={update.apply}
          >
            Reload
          </button>
        </div>
      ) : null}
    </div>
  );
}
