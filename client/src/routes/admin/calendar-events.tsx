import { useState } from "react";
import { AdminFormDialog } from "../../components/admin/admin-form-dialog";
import { AdminScreen } from "../../components/admin/admin-screen";
import { type AdminColumn, AdminTable } from "../../components/admin/admin-table";
import { Button } from "../../components/ui/button";
import { Field } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { Select } from "../../components/ui/select";
import {
  labelFor,
  useIndicatorRows,
  useSourceNames,
  useSourceRows,
} from "../../hooks/use-references";
import { adminErrorMessage, adminRows, useAdminCreate, useAdminList } from "../../lib/admin";
import { formatNumber, formatTimestamp } from "../../lib/format";

type CalendarEventRow = {
  id: string;
  sourceId: string;
  indicatorId: string;
  scheduledAt: string;
  releasedAt: string | null;
  previous: number | null;
  consensus: number | null;
  actual: number | null;
  createdAt: string;
  updatedAt: string;
};

type CreateCalendarEventBody = {
  sourceId: string;
  indicatorId: string;
  scheduledAt: string;
  releasedAt?: string;
  previous?: number;
  consensus?: number;
  actual?: number;
  entityCodes?: string[];
};

const eventsKey = ["admin", "calendar-events"];

export function CalendarEventsScreen() {
  const list = useAdminList<CalendarEventRow>({
    key: eventsKey,
    path: "/api/v1/admin/calendar-events",
    rowsKey: "calendarEvents",
  });
  const [createOpen, setCreateOpen] = useState(false);
  const sourceNames = useSourceNames();
  const indicators = useIndicatorRows();
  const indicatorNames = new Map(indicators.map((indicator) => [indicator.id, indicator.name]));

  const columns: AdminColumn<CalendarEventRow>[] = [
    { header: "Source", cell: (row) => labelFor(sourceNames, row.sourceId) },
    { header: "Indicator", cell: (row) => labelFor(indicatorNames, row.indicatorId) },
    { header: "Scheduled", cell: (row) => formatTimestamp(row.scheduledAt) },
    {
      header: "Released",
      cell: (row) => formatTimestamp(row.releasedAt),
      className: "text-muted-foreground",
    },
    {
      header: "Previous",
      cell: (row) => formatNumber(row.previous),
      className: "text-right tabular-nums",
    },
    {
      header: "Consensus",
      cell: (row) => formatNumber(row.consensus),
      className: "text-right tabular-nums",
    },
    {
      header: "Actual",
      cell: (row) => formatNumber(row.actual),
      className: "text-right tabular-nums",
    },
  ];

  return (
    <AdminScreen
      title="Calendar events"
      description="Scheduled and released figures per source and indicator. Duplicates of the same source, indicator, and time are refused."
      actions={
        <Button className="h-10" onClick={() => setCreateOpen(true)}>
          Create event
        </Button>
      }
    >
      <AdminTable
        columns={columns}
        rows={adminRows(list)}
        loading={list.isPending}
        loadingMore={list.isFetchingNextPage}
        hasNext={list.hasNextPage}
        error={list.error}
        onRetry={() => list.refetch()}
        onLoadMore={() => list.fetchNextPage()}
        emptyLabel="No calendar events yet."
      />
      <CreateEventDialog open={createOpen} onClose={() => setCreateOpen(false)} />
    </AdminScreen>
  );
}

function toISOString(value: string): string | null {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return date.toISOString();
}

function CreateEventDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const create = useAdminCreate<CreateCalendarEventBody>(
    eventsKey,
    "/api/v1/admin/calendar-events",
  );
  const sources = useSourceRows();
  const indicators = useIndicatorRows();
  const [sourceId, setSourceId] = useState("");
  const [indicatorId, setIndicatorId] = useState("");
  const [scheduledAt, setScheduledAt] = useState("");
  const [releasedAt, setReleasedAt] = useState("");
  const [previous, setPrevious] = useState("");
  const [consensus, setConsensus] = useState("");
  const [actual, setActual] = useState("");
  const [entityCodes, setEntityCodes] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  function reset() {
    setSourceId("");
    setIndicatorId("");
    setScheduledAt("");
    setReleasedAt("");
    setPrevious("");
    setConsensus("");
    setActual("");
    setEntityCodes("");
    setLocalError(null);
    create.reset();
  }

  function close() {
    reset();
    onClose();
  }

  function optionalNumber(value: string): number | undefined {
    if (!value.trim()) return undefined;
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : Number.NaN;
  }

  function submit() {
    if (!sourceId || !indicatorId) {
      setLocalError("Source and indicator are required.");
      return;
    }
    const scheduled = toISOString(scheduledAt);
    if (!scheduled) {
      setLocalError("A scheduled time is required.");
      return;
    }
    const released = releasedAt ? toISOString(releasedAt) : null;
    if (releasedAt && !released) {
      setLocalError("The release time is not a valid date.");
      return;
    }
    const previousValue = optionalNumber(previous);
    const consensusValue = optionalNumber(consensus);
    const actualValue = optionalNumber(actual);
    if (
      [previousValue, consensusValue, actualValue].some(
        (value) => value !== undefined && Number.isNaN(value),
      )
    ) {
      setLocalError("Values must be numbers.");
      return;
    }
    const codes = entityCodes
      .split(",")
      .map((code) => code.trim())
      .filter(Boolean);

    setLocalError(null);
    create.mutate(
      {
        sourceId,
        indicatorId,
        scheduledAt: scheduled,
        ...(released ? { releasedAt: released } : {}),
        ...(previousValue !== undefined ? { previous: previousValue } : {}),
        ...(consensusValue !== undefined ? { consensus: consensusValue } : {}),
        ...(actualValue !== undefined ? { actual: actualValue } : {}),
        ...(codes.length > 0 ? { entityCodes: codes } : {}),
      },
      { onSuccess: close },
    );
  }

  return (
    <AdminFormDialog
      open={open}
      onClose={close}
      title="Create calendar event"
      description="Values are optional; the scheduled time is required."
      pending={create.isPending}
      errorMessage={localError ?? adminErrorMessage(create.error)}
      onSubmit={submit}
    >
      <Field label="Source" htmlFor="event-source">
        <Select
          id="event-source"
          value={sourceId}
          onChange={(event) => {
            setSourceId(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        >
          <option value="">Select a source</option>
          {sources.map((source) => (
            <option key={source.id} value={source.id}>
              {source.name}
            </option>
          ))}
        </Select>
      </Field>
      <Field label="Indicator" htmlFor="event-indicator">
        <Select
          id="event-indicator"
          value={indicatorId}
          onChange={(event) => {
            setIndicatorId(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        >
          <option value="">Select an indicator</option>
          {indicators.map((indicator) => (
            <option key={indicator.id} value={indicator.id}>
              {indicator.name}
            </option>
          ))}
        </Select>
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Scheduled at" htmlFor="event-scheduled">
          <Input
            id="event-scheduled"
            type="datetime-local"
            value={scheduledAt}
            onChange={(event) => {
              setScheduledAt(event.target.value);
              setLocalError(null);
              create.reset();
            }}
          />
        </Field>
        <Field label="Released at" htmlFor="event-released" hint="Optional.">
          <Input
            id="event-released"
            type="datetime-local"
            value={releasedAt}
            onChange={(event) => {
              setReleasedAt(event.target.value);
              setLocalError(null);
              create.reset();
            }}
          />
        </Field>
        <Field label="Previous" htmlFor="event-previous">
          <Input
            id="event-previous"
            inputMode="decimal"
            value={previous}
            placeholder="250000"
            onChange={(event) => {
              setPrevious(event.target.value);
              setLocalError(null);
              create.reset();
            }}
          />
        </Field>
        <Field label="Consensus" htmlFor="event-consensus">
          <Input
            id="event-consensus"
            inputMode="decimal"
            value={consensus}
            placeholder="255000"
            onChange={(event) => {
              setConsensus(event.target.value);
              setLocalError(null);
              create.reset();
            }}
          />
        </Field>
        <Field label="Actual" htmlFor="event-actual">
          <Input
            id="event-actual"
            inputMode="decimal"
            value={actual}
            placeholder="261000"
            onChange={(event) => {
              setActual(event.target.value);
              setLocalError(null);
              create.reset();
            }}
          />
        </Field>
        <Field
          label="Entity codes"
          htmlFor="event-entities"
          hint="Optional, comma separated — US, CHINA."
        >
          <Input
            id="event-entities"
            value={entityCodes}
            placeholder="US"
            onChange={(event) => {
              setEntityCodes(event.target.value);
              setLocalError(null);
              create.reset();
            }}
          />
        </Field>
      </div>
    </AdminFormDialog>
  );
}
