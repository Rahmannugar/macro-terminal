import { AdminScreen } from "../components/admin/admin-screen";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { adminErrorMessage, adminRows, useAdminList } from "../lib/admin";
import { formatNumber, formatTimestamp } from "../lib/format";
import { type CalendarEvent, useExplainEvent } from "../lib/terminal";

export function CalendarRoute() {
  const events = useAdminList<CalendarEvent>({
    key: ["calendar"],
    path: "/api/v1/calendar-events",
    rowsKey: "calendarEvents",
  });
  const explain = useExplainEvent();
  const rows = adminRows(events);

  return (
    <AdminScreen
      title="Calendar"
      description="Upcoming macro releases, soonest first. Ask for an AI read on any release before it lands."
    >
      {events.isError ? (
        <div className="rounded-xl border border-border bg-card px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">{adminErrorMessage(events.error)}</p>
          <Button
            variant="secondary"
            className="mt-4 h-9 px-4 text-sm"
            onClick={() => events.refetch()}
          >
            Retry
          </Button>
        </div>
      ) : events.isPending ? (
        <div className="rounded-xl border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
          Loading…
        </div>
      ) : rows.length === 0 ? (
        <div className="rounded-xl border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
          No upcoming events on the calendar yet.
        </div>
      ) : (
        <>
          <ul className="flex flex-col gap-3">
            {rows.map((event) => {
              const explained = explain.isSuccess && explain.variables === event.id;
              return (
                <li key={event.id} className="rounded-xl border border-border bg-card p-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <p className="text-sm font-semibold">{event.indicator.name}</p>
                        <Badge>{event.source.name}</Badge>
                      </div>
                      <p className="mt-1 text-xs text-muted-foreground">
                        Scheduled {formatTimestamp(event.scheduledAt)}
                        {event.releasedAt
                          ? ` · released ${formatTimestamp(event.releasedAt)}`
                          : ""}
                      </p>
                    </div>
                    <Button
                      variant="secondary"
                      className="h-9 shrink-0 px-4 text-sm"
                      pending={explain.isPending && explain.variables === event.id}
                      pendingLabel="Explaining…"
                      onClick={() => explain.mutate(event.id)}
                    >
                      Explain
                    </Button>
                  </div>

                  <p className="mt-3 flex flex-wrap gap-x-5 gap-y-1 text-sm text-muted-foreground">
                    <span>
                      <span className="font-medium text-foreground">Actual</span>{" "}
                      {formatNumber(event.actual)}
                    </span>
                    <span>
                      <span className="font-medium text-foreground">Forecast</span>{" "}
                      {formatNumber(event.consensus)}
                    </span>
                    <span>
                      <span className="font-medium text-foreground">Previous</span>{" "}
                      {formatNumber(event.previous)}
                    </span>
                  </p>

                  {explained ? (
                    <div className="mt-4 rounded-lg border border-primary/20 bg-primary/5 p-4">
                      <p className="text-xs font-semibold uppercase tracking-wide text-primary">
                        AI explanation
                      </p>
                      <p className="mt-2 whitespace-pre-line text-sm leading-7">
                        {explain.data}
                      </p>
                    </div>
                  ) : null}
                  {explain.isError && explain.variables === event.id ? (
                    <p className="mt-3 text-sm text-red-600">
                      {adminErrorMessage(explain.error)}
                    </p>
                  ) : null}
                </li>
              );
            })}
          </ul>
          {events.hasNextPage || events.isFetchingNextPage ? (
            <div className="mt-3 flex min-h-10 items-center justify-center">
              <Button
                variant="secondary"
                className="h-10 px-5 text-sm"
                pending={events.isFetchingNextPage}
                onClick={() => events.fetchNextPage()}
              >
                Load more
              </Button>
            </div>
          ) : (
            <p className="mt-3 text-center text-xs text-muted-foreground">End of list</p>
          )}
        </>
      )}
    </AdminScreen>
  );
}
