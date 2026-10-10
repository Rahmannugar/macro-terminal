import { useState } from "react";
import { AdminScreen } from "../components/admin/admin-screen";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Select } from "../components/ui/select";
import { adminErrorMessage, adminRows, useAdminList } from "../lib/admin";
import { formatNumber, formatTimestamp } from "../lib/format";
import { type CalendarEvent, useExplainEvent } from "../lib/terminal";

type Scope = "upcoming" | "released";

type CalendarFilters = {
  country: string;
  importance: string;
  watch: boolean;
};

const filtersStorageKey = "macro-terminal.calendar.filters";

const defaultFilters: CalendarFilters = { country: "", importance: "", watch: false };

const countryNames: Record<string, string> = {
  AU: "Australia",
  CA: "Canada",
  CH: "Switzerland",
  CN: "China",
  EU: "Euro area",
  GB: "United Kingdom",
  JP: "Japan",
  NZ: "New Zealand",
  US: "United States",
};

function loadCalendarFilters(): CalendarFilters {
  try {
    const raw = localStorage.getItem(filtersStorageKey);
    if (!raw) return defaultFilters;
    const parsed = JSON.parse(raw) as Partial<CalendarFilters>;
    return {
      country:
        typeof parsed.country === "string" && parsed.country.toUpperCase() in countryNames
          ? parsed.country.toUpperCase()
          : "",
      importance:
        typeof parsed.importance === "string" &&
        ["high", "medium", "low"].includes(parsed.importance)
          ? parsed.importance
          : "",
      watch: parsed.watch === true,
    };
  } catch {
    return defaultFilters;
  }
}

function flagOf(code: string): string {
  if (!/^[A-Z]{2}$/.test(code)) return "";
  return String.fromCodePoint(
    ...Array.from(code, (letter) => 0x1f1e6 + letter.charCodeAt(0) - 65),
  );
}

export function CalendarRoute() {
  const [scope, setScope] = useState<Scope>("upcoming");
  const [filters, setFilters] = useState<CalendarFilters>(loadCalendarFilters);
  const explain = useExplainEvent();

  function updateFilters(next: CalendarFilters) {
    setFilters(next);
    try {
      localStorage.setItem(filtersStorageKey, JSON.stringify(next));
    } catch {
      return;
    }
  }

  const sharedFilters = {
    country: filters.country,
    importance: filters.importance,
    watch: filters.watch ? "1" : "",
  };

  const upcoming = useAdminList<CalendarEvent>({
    key: ["calendar", "upcoming"],
    path: "/api/v1/calendar-events",
    rowsKey: "calendarEvents",
    filters: { ...sharedFilters, scope: "" },
    enabled: scope === "upcoming",
  });
  const released = useAdminList<CalendarEvent>({
    key: ["calendar", "released"],
    path: "/api/v1/calendar-events",
    rowsKey: "calendarEvents",
    filters: { ...sharedFilters, scope: "released" },
    enabled: scope === "released",
  });
  const events = scope === "released" ? released : upcoming;
  const rows = adminRows(events);

  const noFilters = !filters.watch && !filters.importance && !filters.country;

  return (
    <AdminScreen
      title="Calendar"
      description="Macro releases, soonest first. Filter by country, importance, or your watch list."
      actions={
        <div className="flex gap-2">
          {(["upcoming", "released"] as const).map((option) => (
            <Button
              key={option}
              variant={scope === option ? "primary" : "secondary"}
              className="h-9 px-4 text-sm"
              aria-pressed={scope === option}
              onClick={() => setScope(option)}
            >
              {option === "upcoming" ? "Upcoming" : "Released"}
            </Button>
          ))}
        </div>
      }
    >
      <div className="flex flex-wrap items-center gap-2">
        <Select
          className="h-9 w-48"
          aria-label="Country"
          value={filters.country}
          onChange={(event) => updateFilters({ ...filters, country: event.target.value })}
        >
          <option value="">All countries</option>
          {Object.entries(countryNames)
            .sort((a, b) => a[1].localeCompare(b[1]))
            .map(([code, name]) => (
              <option key={code} value={code}>
                {flagOf(code)} {name}
              </option>
            ))}
        </Select>
        <Select
          className="h-9 w-44"
          aria-label="Importance"
          value={filters.importance}
          onChange={(event) => updateFilters({ ...filters, importance: event.target.value })}
        >
          <option value="">All importance</option>
          <option value="high">High</option>
          <option value="medium">Medium</option>
          <option value="low">Low</option>
        </Select>
        <Button
          variant={filters.watch ? "primary" : "secondary"}
          className="h-9 px-3 text-xs"
          aria-pressed={filters.watch}
          onClick={() => updateFilters({ ...filters, watch: !filters.watch })}
        >
          My watch list
        </Button>
      </div>

      {events.isError ? (
        <div className="mt-4 rounded-xl border border-border bg-card px-4 py-10 text-center">
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
        <div className="mt-4 flex justify-center rounded-xl border border-border bg-card px-4 py-10">
          <span
            aria-hidden="true"
            className="size-5 animate-spin rounded-full border-2 border-border border-t-primary"
          />
        </div>
      ) : rows.length === 0 ? (
        <div className="mt-4 rounded-xl border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
          {noFilters
            ? scope === "upcoming"
              ? "Nothing scheduled yet."
              : "Nothing released yet."
            : "No events match your filters."}
        </div>
      ) : (
        <>
          <ul className="mt-4 flex flex-col gap-3">
            {rows.map((event) => {
              const explained = explain.isSuccess && explain.variables === event.id;
              return (
                <li key={event.id} className="rounded-xl border border-border bg-card p-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        {event.countryCode ? (
                          <span
                            aria-hidden="true"
                            title={countryNames[event.countryCode] ?? event.countryCode}
                          >
                            {flagOf(event.countryCode)}
                          </span>
                        ) : null}
                        <p className="text-sm font-semibold">
                          {event.name || event.indicator.name}
                        </p>
                        {event.importance === "high" ? <Badge tone="danger">High</Badge> : null}
                        {event.importance === "medium" ? <Badge>Medium</Badge> : null}
                        {event.revision > 0 ? <Badge tone="primary">Revised</Badge> : null}
                        <Badge>{event.source.name}</Badge>
                      </div>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {scope === "released"
                          ? `Released ${formatTimestamp(event.releasedAt ?? event.scheduledAt)}`
                          : `Scheduled ${formatTimestamp(event.scheduledAt)}`}
                        {event.currency ? ` · ${event.currency}` : ""}
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
                        Explanation
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
          <p className="mt-6 text-center text-xs text-muted-foreground">
            Release calendar by{" "}
            <a
              className="underline underline-offset-2 hover:text-foreground"
              href="https://xoomar.com/markets/api/calendar"
              target="_blank"
              rel="noreferrer"
            >
              Xoomar
            </a>
          </p>
        </>
      )}
    </AdminScreen>
  );
}
