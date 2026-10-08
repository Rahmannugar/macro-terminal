import { Link } from "react-router";
import { ArticleList } from "../components/article-list";
import { useAccount } from "../hooks/use-account";
import { adminErrorMessage, adminRows, useAdminList } from "../lib/admin";
import { formatTimestamp } from "../lib/format";
import type { ArticleSummary, CalendarEvent } from "../lib/terminal";
import { useFollowedPairs } from "../lib/watchlist";

export function HomeRoute() {
  const account = useAccount();
  const data = account.data;
  const followed = useFollowedPairs();
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

  if (!data) return null;

  const isAdmin = data.account.role === "admin";
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
      <div className="mt-6 flex flex-wrap gap-3 text-sm">
        <span className="rounded-full bg-secondary px-3 py-1 font-medium">
          {data.account.role}
        </span>
        <span className="rounded-full bg-secondary px-3 py-1">{data.account.status}</span>
      </div>

      <section className="mt-10">
        <div className="flex items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">Watch list</h2>
          <Link
            to="/watchlist"
            className="text-xs font-medium text-primary transition-colors hover:underline"
          >
            Manage
          </Link>
        </div>
        {followed.isPending ? (
          <p className="mt-3 text-sm text-muted-foreground">Loading…</p>
        ) : followed.isError ? (
          <p className="mt-3 text-sm text-muted-foreground">
            {adminErrorMessage(followed.error)}
          </p>
        ) : followedRows.length === 0 ? (
          <p className="mt-3 max-w-[560px] text-sm text-muted-foreground">
            Nothing followed yet.{" "}
            <Link to="/watchlist" className="text-primary transition-colors hover:underline">
              Follow asset pairs
            </Link>{" "}
            to shape the coverage below.
          </p>
        ) : (
          <div className="mt-3 flex flex-wrap gap-2">
            {followedRows.map((pair) => (
              <Link
                key={pair.id}
                to="/watchlist"
                className="rounded-full border border-border bg-card px-3 py-1 text-sm font-medium transition-colors hover:border-primary/40 hover:bg-secondary/60"
              >
                {pair.symbol}
              </Link>
            ))}
          </div>
        )}
      </section>

      <section className="mt-10 max-w-[720px]">
        <div className="flex items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">
            {followedRows.length > 0
              ? "Latest for your watch list"
              : "Latest from the terminal"}
          </h2>
          <Link
            to="/search"
            className="text-xs font-medium text-primary transition-colors hover:underline"
          >
            Search
          </Link>
        </div>
        <div className="mt-3">
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
        </div>
      </section>

      <section className="mt-10 max-w-[720px]">
        <div className="flex items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">Upcoming on the calendar</h2>
          <Link
            to="/calendar"
            className="text-xs font-medium text-primary transition-colors hover:underline"
          >
            Open calendar
          </Link>
        </div>
        <div className="mt-3">
          {calendar.isPending ? (
            <div className="rounded-xl border border-border bg-card px-4 py-6 text-center text-sm text-muted-foreground">
              Loading…
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
      </section>

      <div className="mt-10">
        <h2 className="text-sm font-semibold">Terminal</h2>
        <div className="mt-3 grid max-w-[720px] gap-3 sm:grid-cols-2">
          <Shortcut
            to="/search"
            title="Search"
            description="Semantic search across the article archive."
          />
        </div>
      </div>

      {isAdmin ? (
        <div className="mt-10">
          <h2 className="text-sm font-semibold">Admin</h2>
          <div className="mt-3 grid max-w-[720px] gap-3 sm:grid-cols-2">
            <Shortcut
              to="/admin/users"
              title="Users"
              description="Accounts, roles, suspension."
            />
            <Shortcut
              to="/admin/jobs"
              title="Failed jobs"
              description="Retry exhausted work."
            />
            <Shortcut
              to="/admin/entities"
              title="Configuration"
              description="Entities, pairs, indicators, terms."
            />
            <Shortcut
              to="/admin/source-configurations"
              title="Sources"
              description="Configurations and fetch payloads."
            />
          </div>
        </div>
      ) : null}
    </div>
  );
}

function Shortcut({
  to,
  title,
  description,
}: {
  to: string;
  title: string;
  description: string;
}) {
  return (
    <Link
      to={to}
      className="rounded-xl border border-border bg-card p-4 transition-colors hover:border-primary/40 hover:bg-secondary/60"
    >
      <p className="text-sm font-semibold">{title}</p>
      <p className="mt-1 text-xs text-muted-foreground">{description}</p>
    </Link>
  );
}
