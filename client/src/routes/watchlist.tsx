import { useState } from "react";
import { AdminScreen } from "../components/admin/admin-screen";
import { PairPickerDialog } from "../components/pair-picker-dialog";
import { Button } from "../components/ui/button";
import { adminErrorMessage, adminRows } from "../lib/admin";
import { useFollowedPairs, useToggleFollow } from "../lib/watchlist";

export function WatchListRoute() {
  const followed = useFollowedPairs();
  const toggle = useToggleFollow();
  const [pickerOpen, setPickerOpen] = useState(false);

  const followedRows = adminRows(followed);

  return (
    <AdminScreen
      title="Watch list"
      description="Follow asset pairs to shape your home feed with the coverage that matters to you."
    >
      <section>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">Your pairs</h2>
          <Button
            variant="secondary"
            className="h-9 px-4 text-sm"
            onClick={() => setPickerOpen(true)}
          >
            Pick pairs
          </Button>
        </div>
        {followed.isError ? (
          <p className="mt-3 text-sm text-muted-foreground">
            {adminErrorMessage(followed.error)}
          </p>
        ) : followed.isPending ? (
          <div className="mt-3 flex flex-wrap gap-2">
            {["chip-1", "chip-2", "chip-3"].map((key) => (
              <div key={key} className="h-8 w-20 animate-pulse rounded-full bg-secondary" />
            ))}
          </div>
        ) : followedRows.length === 0 ? (
          <div className="mt-3 rounded-xl border border-dashed border-border px-4 py-6 text-center">
            <p className="text-sm text-muted-foreground">
              You are not following any pairs yet. Pick pairs to personalize your feed.
            </p>
          </div>
        ) : (
          <div className="mt-3 flex flex-wrap gap-2">
            {followedRows.map((pair) => {
              const pending = toggle.isPending && toggle.variables?.pairId === pair.id;
              return (
                <span
                  key={pair.id}
                  className="inline-flex items-center gap-1.5 rounded-full border border-border bg-card py-1 pl-3 pr-1.5 text-sm font-medium"
                >
                  {pair.symbol}
                  <button
                    type="button"
                    aria-label={`Unfollow ${pair.symbol}`}
                    disabled={pending}
                    className="grid size-6 place-items-center rounded-full text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-50"
                    onClick={() => toggle.mutate({ pairId: pair.id, following: false })}
                  >
                    <svg
                      aria-hidden="true"
                      viewBox="0 0 16 16"
                      className="size-3"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.5"
                    >
                      <path d="m4 4 8 8M12 4l-8 8" strokeLinecap="round" />
                    </svg>
                  </button>
                </span>
              );
            })}
          </div>
        )}
      </section>

      {pickerOpen ? (
        <PairPickerDialog
          followedIds={new Set(followedRows.map((pair) => pair.id))}
          onClose={() => setPickerOpen(false)}
        />
      ) : null}
    </AdminScreen>
  );
}
