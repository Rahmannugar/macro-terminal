import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { ArticleList } from "../components/article-list";
import { PairPickerDialog } from "../components/pair-picker-dialog";
import { Button } from "../components/ui/button";
import { SkeletonList } from "../components/ui/skeleton";
import { useAccount } from "../hooks/use-account";
import { useLiveFeed } from "../hooks/use-live-feed";
import { adminErrorMessage, adminRows, requestAdminPage, useAdminList } from "../lib/admin";
import { formatTimestamp } from "../lib/format";
import type { ArticleSummary, CalendarEvent } from "../lib/terminal";
import { useFollowedPairs, useToggleFollow } from "../lib/watchlist";

export function HomeRoute() {
  const account = useAccount();
  const data = account.data;
  const queryClient = useQueryClient();
  const [newCount, setNewCount] = useState<number | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const followed = useFollowedPairs();
  const toggle = useToggleFollow();
  const feed = useAdminList<ArticleSummary>({
    key: ["feed"],
    path: "/api/v1/articles",
    rowsKey: "articles",
  });
  const calendar = useAdminList<CalendarEvent>({
    key: ["calendar"],
    path: "/api/v1/calendar-events",
    rowsKey: "calendarEvents",
  });

  useLiveFeed(async () => {
    const loadedIds = new Set(adminRows(feed).map((article) => article.id));
    try {
      const page = await requestAdminPage<ArticleSummary>(
        "/api/v1/articles",
        "articles",
        undefined,
        null,
      );
      const fresh = page.rows.filter((article) => !loadedIds.has(article.id)).length;
      if (fresh > 0) setNewCount(fresh);
    } catch {
      // A failed count must not disturb the open feed; the signal fires again
      // on the next stored batch.
    }
  }, !!data);

  if (!data) return null;

  const applyFeedRefresh = () => {
    setNewCount(null);
    queryClient.resetQueries({ queryKey: ["feed"] });
  };

  const followedRows = adminRows(followed);
  const upcoming = adminRows(calendar).slice(0, 5);

  return (
    <div>
      <h1 className="font-display text-[28px] font-semibold tracking-[-0.04em]">
        Welcome back{data.account.username ? `, ${data.account.username}` : ""}
      </h1>
      <p className="mt-2 max-w-[560px] text-sm text-muted-foreground">
        Macro intelligence and fundamental research terminal.
      </p>

      <section className="mt-10">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">Watch list</h2>
          <Button
            variant="secondary"
            className="h-9 px-4 text-sm"
            onClick={() => setPickerOpen(true)}
          >
            Pick pairs
          </Button>
        </div>
        {followed.isPending ? (
          <div className="mt-3 flex flex-wrap gap-2">
            {["chip-1", "chip-2", "chip-3", "chip-4"].map((key) => (
              <div key={key} className="h-8 w-20 animate-pulse rounded-full bg-secondary" />
            ))}
          </div>
        ) : followed.isError ? (
          <p className="mt-3 text-sm text-muted-foreground">
            {adminErrorMessage(followed.error)}
          </p>
        ) : followedRows.length === 0 ? (
          <div className="mt-3 rounded-xl border border-dashed border-border px-4 py-6 text-center">
            <p className="text-sm text-muted-foreground">
              Nothing followed yet. Pick pairs to shape your feed.
            </p>
          </div>
        ) : (
          <div className="mt-3 flex flex-wrap gap-2">
            {followedRows.map((pair) => (
              <span
                key={pair.id}
                className="inline-flex items-center gap-1.5 rounded-full border border-border bg-card py-1 pl-3 pr-1.5 text-sm font-medium"
              >
                {pair.symbol}
                <button
                  type="button"
                  aria-label={`Unfollow ${pair.symbol}`}
                  disabled={toggle.isPending && toggle.variables?.pairId === pair.id}
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
            ))}
          </div>
        )}
      </section>

      <div className="mt-10 flex flex-col gap-10 lg:flex-row lg:items-start lg:gap-8">
        <section className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">
            {followedRows.length > 0
              ? "Latest for your watch list"
              : "Latest from the terminal"}
          </h2>
          <div className="mt-3">
            {newCount !== null && newCount > 0 ? (
              <button
                type="button"
                onClick={applyFeedRefresh}
                className="mb-3 flex w-full items-center justify-center gap-2 rounded-full border border-primary/30 bg-primary/10 px-4 py-2 text-sm font-medium text-primary transition-colors hover:bg-primary/15"
              >
                {newCount === 1 ? "1 new article" : `${newCount} new articles`}
              </button>
            ) : null}
            {feed.isPending ? (
              <SkeletonList rows={4} />
            ) : (
              <ArticleList
                articles={adminRows(feed)}
                linkState={{ from: "/" }}
                loading={feed.isPending}
                loadingMore={feed.isFetchingNextPage}
                hasNext={feed.hasNextPage}
                error={feed.error}
                onRetry={() => feed.refetch()}
                onLoadMore={() => feed.fetchNextPage()}
                emptyLabel="No articles have been indexed yet."
              />
            )}
          </div>
        </section>

        <aside className="w-full shrink-0 lg:w-80">
          <h2 className="text-sm font-semibold">Upcoming on the calendar</h2>
          <div className="mt-3">
            {calendar.isPending ? (
              <div className="flex flex-col gap-2">
                {["row-1", "row-2", "row-3"].map((key) => (
                  <div key={key} className="h-14 animate-pulse rounded-xl bg-secondary" />
                ))}
              </div>
            ) : calendar.isError ? (
              <div className="rounded-xl border border-border bg-card px-4 py-6 text-center text-sm text-muted-foreground">
                {adminErrorMessage(calendar.error)}
              </div>
            ) : upcoming.length === 0 ? (
              <div className="rounded-xl border border-border bg-card px-4 py-6 text-center text-sm text-muted-foreground">
                No upcoming events scheduled.
              </div>
            ) : (
              <ul className="flex flex-col gap-2">
                {upcoming.map((event) => (
                  <li key={event.id}>
                    <Link
                      to="/calendar"
                      className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 rounded-xl border border-border bg-card px-4 py-3 transition-colors hover:border-primary/40 hover:bg-secondary/60"
                    >
                      <span className="text-sm font-semibold">{event.indicator.name}</span>
                      <span className="text-xs text-muted-foreground">
                        {event.source.name} · {formatTimestamp(event.scheduledAt)}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </aside>
      </div>

      {pickerOpen ? (
        <PairPickerDialog
          followedIds={new Set(followedRows.map((pair) => pair.id))}
          onClose={() => setPickerOpen(false)}
        />
      ) : null}
    </div>
  );
}
