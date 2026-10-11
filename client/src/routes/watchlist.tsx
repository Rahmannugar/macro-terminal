import { CheckIcon, PlusIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { AdminScreen } from "../components/admin/admin-screen";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Skeleton } from "../components/ui/skeleton";
import { adminErrorMessage, adminRows } from "../lib/admin";
import { useFollowedPairs, usePairCatalog, useToggleFollow } from "../lib/watchlist";

export function WatchListRoute() {
  const followed = useFollowedPairs();
  const catalog = usePairCatalog();
  const toggle = useToggleFollow();
  const [query, setQuery] = useState("");

  const followedIds = new Set(adminRows(followed).map((pair) => pair.id));
  const search = query
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]/g, "");
  const catalogRows = adminRows(catalog)
    .filter(
      (pair) =>
        !search ||
        pair.symbol
          .toLowerCase()
          .replace(/[^a-z0-9]/g, "")
          .includes(search),
    )
    .toSorted((a, b) => a.symbol.localeCompare(b.symbol));

  return (
    <AdminScreen
      title="Watch list"
      description="Follow asset pairs to shape your home feed with the coverage that matters to you."
    >
      <div className="mt-4 mb-5 max-w-sm">
        <Input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search pairs"
          aria-label="Search pairs"
        />
      </div>
      {catalog.isError ? (
        <p className="mt-3 text-sm text-muted-foreground">{adminErrorMessage(catalog.error)}</p>
      ) : catalog.isPending ? (
        <div className="mt-3 flex flex-col gap-2">
          {["row-1", "row-2", "row-3", "row-4", "row-5"].map((key) => (
            <Skeleton key={key} className="h-11 rounded-lg" />
          ))}
        </div>
      ) : (
        <>
          <ul className="mt-1 flex flex-col gap-3">
            {catalogRows.map((pair) => {
              const following = followedIds.has(pair.id);
              const pending = toggle.isPending && toggle.variables?.pairId === pair.id;
              return (
                <li
                  key={pair.id}
                  className="flex items-center justify-between gap-3 rounded-lg border border-border bg-card px-4 py-2.5"
                >
                  <span className="text-sm font-medium">{pair.symbol}</span>
                  <Button
                    variant={following ? "secondary" : "primary"}
                    className="h-8 min-w-24 justify-center gap-1.5 px-3 text-xs"
                    disabled={pending}
                    onClick={() => toggle.mutate({ pairId: pair.id, following: !following })}
                  >
                    {following ? (
                      <>
                        <CheckIcon aria-hidden="true" className="size-3.5" />
                        Following
                      </>
                    ) : (
                      <>
                        <PlusIcon aria-hidden="true" className="size-3.5" />
                        Follow
                      </>
                    )}
                  </Button>
                </li>
              );
            })}
          </ul>
          {catalog.hasNextPage && !catalog.isFetchingNextPage ? (
            <div className="mt-3 flex justify-center">
              <Button
                variant="quiet"
                className="h-9 px-3 text-xs"
                onClick={() => catalog.fetchNextPage()}
              >
                Load more
              </Button>
            </div>
          ) : null}
          {!catalog.hasNextPage ? (
            <p className="mt-3 text-center text-xs text-muted-foreground">End of list</p>
          ) : null}
        </>
      )}
    </AdminScreen>
  );
}
