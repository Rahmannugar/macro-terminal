import { useState } from "react";
import { adminErrorMessage, adminRows } from "../lib/admin";
import { usePairCatalog, useToggleFollow } from "../lib/watchlist";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Input } from "./ui/input";
import { Skeleton } from "./ui/skeleton";

// PairPickerDialog is the one surface for editing the watch list: search,
// multi-select, confirm once. Mounted fresh per open so the selection always
// starts from the currently followed set.
export function PairPickerDialog({
  onClose,
  followedIds,
}: {
  onClose: () => void;
  followedIds: Set<string>;
}) {
  const catalog = usePairCatalog();
  const toggle = useToggleFollow();
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<Set<string>>(() => new Set(followedIds));
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const search = query.trim().toLowerCase();
  const rows = adminRows(catalog)
    .filter((pair) => !search || pair.symbol.toLowerCase().includes(search))
    .toSorted((a, b) => a.symbol.localeCompare(b.symbol));

  function togglePair(pairId: string) {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(pairId)) {
        next.delete(pairId);
      } else {
        next.add(pairId);
      }
      return next;
    });
  }

  async function confirm() {
    if (applying) return;
    const toFollow = [...selected].filter((id) => !followedIds.has(id));
    const toUnfollow = [...followedIds].filter((id) => !selected.has(id));
    if (toFollow.length === 0 && toUnfollow.length === 0) {
      onClose();
      return;
    }
    setApplying(true);
    setError(null);
    try {
      for (const pairId of toFollow) {
        await toggle.mutateAsync({ pairId, following: true });
      }
      for (const pairId of toUnfollow) {
        await toggle.mutateAsync({ pairId, following: false });
      }
      onClose();
    } catch {
      setError("Some pairs could not be updated. Try again.");
    } finally {
      setApplying(false);
    }
  }

  return (
    <Dialog
      open
      onClose={() => {
        if (!applying) onClose();
      }}
      title="Pick pairs"
      description="Select the pairs that shape your feed and calendar, then confirm once."
    >
      <div className="mt-4">
        <Input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search pairs"
          aria-label="Search pairs"
        />
      </div>
      <div className="mt-3 max-h-[46vh] overflow-y-auto rounded-xl border border-border">
        {catalog.isPending ? (
          <div className="flex flex-col gap-2 p-3">
            {["row-1", "row-2", "row-3", "row-4"].map((key) => (
              <Skeleton key={key} className="h-11 rounded-lg" />
            ))}
          </div>
        ) : catalog.isError ? (
          <p className="p-4 text-sm text-muted-foreground">
            {adminErrorMessage(catalog.error)}
          </p>
        ) : rows.length === 0 ? (
          <p className="p-4 text-sm text-muted-foreground">
            {search ? `No pairs matched “${query.trim()}”.` : "No pairs are available yet."}
          </p>
        ) : (
          <ul className="divide-y divide-border">
            {rows.map((pair) => (
              <li key={pair.id}>
                <label className="flex cursor-pointer items-center gap-3 px-4 py-2.5 transition-colors hover:bg-secondary">
                  <input
                    type="checkbox"
                    className="size-4 accent-primary"
                    checked={selected.has(pair.id)}
                    onChange={() => togglePair(pair.id)}
                  />
                  <span className="text-sm font-medium">{pair.symbol}</span>
                </label>
              </li>
            ))}
          </ul>
        )}
      </div>
      {catalog.hasNextPage && !search ? (
        <div className="mt-2 flex justify-center">
          <Button
            variant="quiet"
            className="h-9 px-3 text-xs"
            pending={catalog.isFetchingNextPage}
            onClick={() => catalog.fetchNextPage()}
          >
            Load more
          </Button>
        </div>
      ) : null}
      {error ? (
        <p role="alert" className="mt-3 text-sm text-red-600">
          {error}
        </p>
      ) : null}
      <div className="mt-4 flex items-center justify-between gap-3">
        <span className="text-xs text-muted-foreground">{selected.size} selected</span>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={onClose} disabled={applying}>
            Cancel
          </Button>
          <Button onClick={() => void confirm()} pending={applying}>
            Confirm
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
