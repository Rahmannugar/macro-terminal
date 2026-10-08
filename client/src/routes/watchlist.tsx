import { useState } from "react";
import { AdminScreen } from "../components/admin/admin-screen";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { adminErrorMessage, adminRows } from "../lib/admin";
import {
  type EntityPair,
  useFollowedPairs,
  usePairCatalog,
  useToggleFollow,
} from "../lib/watchlist";

export function WatchListRoute() {
  const followed = useFollowedPairs();
  const catalog = usePairCatalog();
  const toggle = useToggleFollow();
  const [filter, setFilter] = useState("");

  const followedRows = adminRows(followed);
  const followedIDs = new Set(followedRows.map((pair) => pair.id));
  const query = filter.trim().toLowerCase();
  const catalogRows = adminRows(catalog).filter(
    (pair) => !query || pair.symbol.toLowerCase().includes(query),
  );

  return (
    <AdminScreen
      title="Watch list"
      description="Follow asset pairs to shape your home feed with the coverage that matters to you."
    >
      <section>
        <h2 className="text-sm font-semibold">Your pairs</h2>
        {followed.isError ? (
          <p className="mt-3 text-sm text-muted-foreground">
            {adminErrorMessage(followed.error)}
          </p>
        ) : followed.isPending ? (
          <p className="mt-3 text-sm text-muted-foreground">Loading…</p>
        ) : followedRows.length === 0 ? (
          <p className="mt-3 text-sm text-muted-foreground">
            You are not following any pairs yet. Follow one below to personalize your feed.
          </p>
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

      <section className="mt-10">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">Browse pairs</h2>
          <div className="w-full max-w-[280px]">
            <Input
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
              placeholder="Filter by symbol"
              aria-label="Filter pairs by symbol"
            />
          </div>
        </div>

        <div className="mt-3">
          {catalog.isError ? (
            <div className="rounded-xl border border-border bg-card px-4 py-10 text-center">
              <p className="text-sm text-muted-foreground">
                {adminErrorMessage(catalog.error)}
              </p>
              <Button
                variant="secondary"
                className="mt-4 h-9 px-4 text-sm"
                onClick={() => catalog.refetch()}
              >
                Retry
              </Button>
            </div>
          ) : catalog.isPending ? (
            <div className="rounded-xl border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
              Loading…
            </div>
          ) : catalogRows.length === 0 ? (
            <div className="rounded-xl border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
              {query ? `No pairs matched “${filter.trim()}”.` : "No pairs are available yet."}
            </div>
          ) : (
            <>
              <ul className="flex flex-col gap-2">
                {catalogRows.map((pair) => (
                  <PairRow
                    key={pair.id}
                    pair={pair}
                    following={followedIDs.has(pair.id)}
                    pending={
                      toggle.isPending && toggle.variables?.pairId === pair.id
                        ? toggle.variables.following
                        : false
                    }
                    onToggle={(following) => toggle.mutate({ pairId: pair.id, following })}
                  />
                ))}
              </ul>
              {catalog.hasNextPage || catalog.isFetchingNextPage ? (
                <div className="mt-3 flex min-h-10 items-center justify-center">
                  <Button
                    variant="secondary"
                    className="h-10 px-5 text-sm"
                    pending={catalog.isFetchingNextPage}
                    onClick={() => catalog.fetchNextPage()}
                  >
                    Load more
                  </Button>
                </div>
              ) : null}
            </>
          )}
        </div>
      </section>
    </AdminScreen>
  );
}

function PairRow({
  pair,
  following,
  pending,
  onToggle,
}: {
  pair: EntityPair;
  following: boolean;
  pending: boolean;
  onToggle: (following: boolean) => void;
}) {
  return (
    <li className="flex items-center justify-between gap-3 rounded-xl border border-border bg-card px-4 py-3">
      <div className="min-w-0">
        <p className="truncate text-sm font-semibold">{pair.symbol}</p>
      </div>
      <Button
        variant={following ? "secondary" : "primary"}
        className="h-9 shrink-0 px-4 text-sm"
        pending={pending}
        pendingLabel={following ? "Removing…" : "Following…"}
        onClick={() => onToggle(!following)}
      >
        {following ? "Following" : "Follow"}
      </Button>
    </li>
  );
}
