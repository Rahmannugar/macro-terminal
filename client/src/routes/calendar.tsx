import { useState } from "react";
import { AdminScreen } from "../components/admin/admin-screen";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Select } from "../components/ui/select";
import { Skeleton } from "../components/ui/skeleton";
import { Tabs } from "../components/ui/tabs";
import { adminErrorMessage, adminRows, useAdminList } from "../lib/admin";
import { formatNumber, formatTimestamp } from "../lib/format";
import { type CalendarEvent, useExplainEvent } from "../lib/terminal";

type Scope = "upcoming" | "released";

type Granularity = "week" | "month";

type CalendarFilters = {
  countries: string[];
  importance: string;
  watch: boolean;
};

function startOfWeek(anchor: Date): Date {
  const start = new Date(anchor);
  start.setHours(0, 0, 0, 0);
  const dayOffset = (start.getDay() + 6) % 7;
  start.setDate(start.getDate() - dayOffset);
  return start;
}

function startOfMonth(anchor: Date): Date {
  const start = new Date(anchor.getFullYear(), anchor.getMonth(), 1);
  start.setHours(0, 0, 0, 0);
  return start;
}

function periodOf(anchor: Date, granularity: Granularity): { start: Date; end: Date } {
  if (granularity === "month") {
    const start = startOfMonth(anchor);
    const end = new Date(start.getFullYear(), start.getMonth() + 1, 1);
    end.setHours(0, 0, 0, 0);
    return { start, end };
  }
  const start = startOfWeek(anchor);
  const end = new Date(start);
  end.setDate(end.getDate() + 7);
  return { start, end };
}

function previousPeriod(anchor: Date, granularity: Granularity): Date {
  if (granularity === "month") {
    const start = startOfMonth(anchor);
    return new Date(start.getFullYear(), start.getMonth() - 1, 1);
  }
  const start = startOfWeek(anchor);
  start.setDate(start.getDate() - 7);
  return start;
}

function nextPeriod(anchor: Date, granularity: Granularity): Date {
  if (granularity === "month") {
    const start = startOfMonth(anchor);
    return new Date(start.getFullYear(), start.getMonth() + 1, 1);
  }
  const start = startOfWeek(anchor);
  start.setDate(start.getDate() + 7);
  return start;
}

function formatPeriodLabel(start: Date, end: Date, granularity: Granularity): string {
  if (granularity === "month") {
    return start.toLocaleDateString(undefined, { month: "long", year: "numeric" });
  }
  const lastDay = new Date(end);
  lastDay.setDate(lastDay.getDate() - 1);
  const monthDay = { month: "short", day: "numeric" } as const;
  return `${start.toLocaleDateString(undefined, monthDay)} – ${lastDay.toLocaleDateString(undefined, monthDay)}, ${lastDay.getFullYear()}`;
}

const filtersStorageKey = "macro-terminal.calendar.filters";

const defaultFilters: CalendarFilters = { countries: [], importance: "", watch: false };

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
    const countries = Array.isArray(parsed.countries)
      ? parsed.countries.filter(
          (code): code is string =>
            typeof code === "string" && code.toUpperCase() in countryNames,
        )
      : [];
    return {
      countries: countries.map((code) => code.toUpperCase()),
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
  const [granularity, setGranularity] = useState<Granularity>("week");
  const [anchor, setAnchor] = useState(() => new Date());
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

  function toggleCountry(code: string) {
    const countries = filters.countries.includes(code)
      ? filters.countries.filter((c) => c !== code)
      : [...filters.countries, code];
    updateFilters({ ...filters, countries });
  }

  const countryParam = filters.countries.join(",");

  const period = periodOf(anchor, granularity);
  const fromParam = period.start.toISOString();
  const toParam = period.end.toISOString();
  const windowFilters = {
    country: countryParam,
    importance: filters.importance,
    watch: filters.watch ? "1" : "",
    from: fromParam,
    to: toParam,
  };

  const upcoming = useAdminList<CalendarEvent>({
    key: ["calendar", "upcoming", countryParam, fromParam, toParam],
    path: "/api/v1/calendar-events",
    rowsKey: "calendarEvents",
    filters: { ...windowFilters, scope: "" },
    enabled: scope === "upcoming",
  });
  const released = useAdminList<CalendarEvent>({
    key: ["calendar", "released", countryParam, fromParam, toParam],
    path: "/api/v1/calendar-events",
    rowsKey: "calendarEvents",
    filters: { ...windowFilters, scope: "released" },
    enabled: scope === "released",
  });
  const events = scope === "released" ? released : upcoming;
  const rows = adminRows(events);

  const noFilters = !filters.watch && !filters.importance && filters.countries.length === 0;

  return (
    <AdminScreen
      title="Calendar"
      description="Browse releases week by week or month by month. Filter by country, importance, or your watch list."
      actions={
        <Tabs
          value={scope}
          options={["upcoming", "released"] as const}
          onChange={setScope}
          label={(option) => (option === "upcoming" ? "Upcoming" : "Released")}
          size="sm"
        />
      }
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <Button
            variant="secondary"
            className="h-9 w-9 px-0"
            aria-label={`Previous ${granularity}`}
            onClick={() => setAnchor(previousPeriod(anchor, granularity))}
          >
            <svg
              aria-hidden="true"
              viewBox="0 0 16 16"
              className="size-4"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
            >
              <path d="m10 3-5 5 5 5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </Button>
          <span className="min-w-40 text-center text-sm font-medium">
            {formatPeriodLabel(period.start, period.end, granularity)}
          </span>
          <Button
            variant="secondary"
            className="h-9 w-9 px-0"
            aria-label={`Next ${granularity}`}
            onClick={() => setAnchor(nextPeriod(anchor, granularity))}
          >
            <svg
              aria-hidden="true"
              viewBox="0 0 16 16"
              className="size-4"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
            >
              <path d="m6 3 5 5-5 5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </Button>
          <Button
            variant="secondary"
            className="h-9 px-3 text-xs"
            onClick={() => setAnchor(new Date())}
          >
            Today
          </Button>
        </div>
        <Tabs
          value={granularity}
          options={["week", "month"] as const}
          onChange={setGranularity}
          label={(option) => (option === "week" ? "Week" : "Month")}
          size="sm"
        />
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <CountryMenu selected={filters.countries} onToggle={(code) => toggleCountry(code)} />
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
        <div className="mt-4 flex flex-col gap-3">
          {["row-1", "row-2", "row-3", "row-4"].map((key) => (
            <Skeleton key={key} className="h-24 rounded-xl" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <div className="mt-4 rounded-xl border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
          {noFilters
            ? scope === "upcoming"
              ? "Nothing scheduled in this period."
              : "Nothing released in this period."
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
                        {event.importance === "medium" ? (
                          <Badge tone="warning">Medium</Badge>
                        ) : null}
                        {event.importance === "low" ? <Badge tone="caution">Low</Badge> : null}
                        {event.revision > 0 ? <Badge tone="primary">Revised</Badge> : null}
                        <Badge>{event.source.name}</Badge>
                      </div>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {event.releasedAt
                          ? `Released ${formatTimestamp(event.releasedAt)}`
                          : `Scheduled ${formatTimestamp(event.scheduledAt)}`}
                        {event.currency ? ` · ${event.currency}` : ""}
                      </p>
                    </div>
                    <Button
                      variant="secondary"
                      className="h-9 shrink-0 px-4 text-sm"
                      pending={explain.isPending && explain.variables === event.id}
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
        </>
      )}
    </AdminScreen>
  );
}

// CountryMenu is a dropdown multi-select: flag plus full country name on
// every row, with the current selection summarized on the trigger.
function CountryMenu({
  selected,
  onToggle,
}: {
  selected: string[];
  onToggle: (code: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const options = Object.entries(countryNames).sort((a, b) => a[1].localeCompare(b[1]));

  const label =
    selected.length === 0
      ? "All countries"
      : selected.length <= 2
        ? selected.map((code) => countryNames[code] ?? code).join(", ")
        : `${selected.length} countries`;

  return (
    <div className="relative">
      <button
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        className={`inline-flex h-9 items-center gap-2 rounded-lg border px-3 text-xs font-medium transition-colors ${
          selected.length > 0
            ? "border-primary bg-primary/10 text-primary"
            : "border-border bg-card text-muted-foreground hover:bg-secondary"
        }`}
        onClick={() => setOpen((value) => !value)}
      >
        {label}
        <svg
          aria-hidden="true"
          viewBox="0 0 16 16"
          className={`size-3 transition-transform ${open ? "rotate-180" : ""}`}
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <path d="m4 6 4 4 4-4" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      {open ? (
        <>
          <div
            className="fixed inset-0 z-40"
            aria-hidden="true"
            onClick={() => setOpen(false)}
          />
          <div
            role="listbox"
            aria-multiselectable="true"
            className="absolute left-0 z-50 mt-1.5 max-h-72 w-56 overflow-y-auto rounded-xl border border-border bg-card p-1.5 shadow-xl"
          >
            {options.map(([code, name]) => {
              const checked = selected.includes(code);
              return (
                <button
                  key={code}
                  type="button"
                  role="option"
                  aria-selected={checked}
                  className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-sm transition-colors hover:bg-secondary"
                  onClick={() => onToggle(code)}
                >
                  <span
                    aria-hidden="true"
                    className={`grid size-4 shrink-0 place-items-center rounded border transition-colors ${
                      checked ? "border-primary bg-primary text-white" : "border-border"
                    }`}
                  >
                    {checked ? (
                      <svg
                        aria-hidden="true"
                        viewBox="0 0 16 16"
                        className="size-3"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                      >
                        <path
                          d="m3.5 8.5 3 3 6-6"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        />
                      </svg>
                    ) : null}
                  </span>
                  <span aria-hidden="true">{flagOf(code)}</span>
                  <span className="truncate">{name}</span>
                </button>
              );
            })}
          </div>
        </>
      ) : null}
    </div>
  );
}
