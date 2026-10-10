import { useState } from "react";
import { AdminFormDialog } from "../../components/admin/admin-form-dialog";
import { AdminScreen } from "../../components/admin/admin-screen";
import { type AdminColumn, AdminTable } from "../../components/admin/admin-table";
import { Badge } from "../../components/ui/badge";
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
import {
  adminErrorMessage,
  adminRows,
  useAdminCreate,
  useAdminList,
  useAdminPost,
  useAdminReplace,
} from "../../lib/admin";
import { formatNumber, formatTimestamp } from "../../lib/format";

type CalendarEventRow = {
  id: string;
  sourceId: string;
  indicatorId: string;
  name: string;
  scheduledAt: string;
  releasedAt: string | null;
  previous: number | null;
  consensus: number | null;
  actual: number | null;
  countryCode: string;
  currency: string;
  importance: string;
  revision: number;
  archivedAt: string | null;
  createdAt: string;
  updatedAt: string;
};

type CreateCalendarEventBody = {
  sourceId: string;
  indicatorId: string;
  name?: string;
  scheduledAt: string;
  releasedAt?: string;
  previous?: number;
  consensus?: number;
  actual?: number;
  entityCodes?: string[];
};

type UpdateCalendarEventBody = {
  name?: string;
  scheduledAt: string;
  releasedAt?: string | null;
  previous?: number | null;
  consensus?: number | null;
  actual?: number | null;
};

const eventsKey = ["admin", "calendar-events"];

export function CalendarEventsScreen() {
  const list = useAdminList<CalendarEventRow>({
    key: eventsKey,
    path: "/api/v1/admin/calendar-events",
    rowsKey: "calendarEvents",
  });
  const archive = useAdminPost(eventsKey);
  const restore = useAdminPost(eventsKey);
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<CalendarEventRow | null>(null);
  const sourceNames = useSourceNames();
  const indicators = useIndicatorRows();
  const indicatorNames = new Map(indicators.map((indicator) => [indicator.id, indicator.name]));

  const columns: AdminColumn<CalendarEventRow>[] = [
    {
      header: "Name",
      cell: (row) => (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="font-medium">{row.name || "—"}</span>
          {row.archivedAt ? <Badge tone="danger">Archived</Badge> : null}
          {row.revision > 0 ? <Badge tone="primary">R{row.revision}</Badge> : null}
        </div>
      ),
    },
    { header: "Country", cell: (row) => row.countryCode || "—" },
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
    {
      header: "",
      cell: (row) => (
        <div className="flex justify-end gap-1">
          <Button
            variant="quiet"
            className="h-8 rounded-md px-2.5 text-xs font-medium"
            onClick={() => setEditing(row)}
          >
            Edit
          </Button>
          {row.archivedAt ? (
            <Button
              variant="quiet"
              className="h-8 rounded-md px-2.5 text-xs font-medium"
              pending={restore.isPending && restore.variables === row.id}
              onClick={() => restore.mutate(`/api/v1/admin/calendar-events/${row.id}/restore`)}
            >
              Restore
            </Button>
          ) : (
            <Button
              variant="quiet"
              className="h-8 rounded-md px-2.5 text-xs font-medium"
              pending={archive.isPending && archive.variables === row.id}
              onClick={() => archive.mutate(`/api/v1/admin/calendar-events/${row.id}/archive`)}
            >
              Archive
            </Button>
          )}
        </div>
      ),
      className: "w-40 text-right",
    },
  ];

  return (
    <AdminScreen
      title="Calendar events"
      description="Scheduled and released figures per source and indicator. Archive hides an event from the user calendar without deleting it."
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
      {(archive.error ?? restore.error) && !archive.isPending && !restore.isPending ? (
        <p className="mt-3 text-sm text-red-600">
          {adminErrorMessage(archive.error ?? restore.error)}
        </p>
      ) : null}
      <CreateEventDialog open={createOpen} onClose={() => setCreateOpen(false)} />
      <EditEventDialog
        key={editing?.id ?? "edit"}
        event={editing}
        onClose={() => setEditing(null)}
      />
    </AdminScreen>
  );
}

function toISOString(value: string): string | null {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return date.toISOString();
}

function toLocalInput(value: string | null): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function parseEventFields(
  scheduledAt: string,
  releasedAt: string,
  previous: string,
  consensus: string,
  actual: string,
  setLocalError: (message: string | null) => void,
): UpdateCalendarEventBody | null {
  const scheduled = toISOString(scheduledAt);
  if (!scheduled) {
    setLocalError("A scheduled time is required.");
    return null;
  }
  const released = releasedAt ? toISOString(releasedAt) : null;
  if (releasedAt && !released) {
    setLocalError("The release time is not a valid date.");
    return null;
  }
  const toOptional = (value: string): number | null => {
    if (!value.trim()) return null;
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : Number.NaN;
  };
  const values = [toOptional(previous), toOptional(consensus), toOptional(actual)];
  if (values.some((value) => value !== undefined && Number.isNaN(value))) {
    setLocalError("Values must be numbers.");
    return null;
  }
  const [previousValue, consensusValue, actualValue] = values;
  return {
    scheduledAt: scheduled,
    releasedAt: released,
    previous: previousValue,
    consensus: consensusValue,
    actual: actualValue,
  };
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
  const [name, setName] = useState("");
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
    setName("");
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

  function submit() {
    if (!sourceId || !indicatorId) {
      setLocalError("Source and indicator are required.");
      return;
    }
    const fields = parseEventFields(
      scheduledAt,
      releasedAt,
      previous,
      consensus,
      actual,
      setLocalError,
    );
    if (!fields) return;
    const codes = entityCodes
      .split(",")
      .map((code) => code.trim())
      .filter(Boolean);

    setLocalError(null);
    create.mutate(
      {
        sourceId,
        indicatorId,
        ...(name.trim() ? { name: name.trim() } : {}),
        scheduledAt: fields.scheduledAt,
        ...(fields.releasedAt ? { releasedAt: fields.releasedAt } : {}),
        ...(fields.previous !== null ? { previous: fields.previous } : {}),
        ...(fields.consensus !== null ? { consensus: fields.consensus } : {}),
        ...(fields.actual !== null ? { actual: fields.actual } : {}),
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
      <div className="grid gap-4 sm:grid-cols-2">
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
        <Field label="Name" htmlFor="event-name" hint="Shown on the user calendar.">
          <Input
            id="event-name"
            value={name}
            placeholder="US CPI"
            onChange={(event) => {
              setName(event.target.value);
              setLocalError(null);
              create.reset();
            }}
          />
        </Field>
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

function EditEventDialog({
  event,
  onClose,
}: {
  event: CalendarEventRow | null;
  onClose: () => void;
}) {
  const update = useAdminReplace<UpdateCalendarEventBody>(
    eventsKey,
    event ? `/api/v1/admin/calendar-events/${event.id}` : "/api/v1/admin/calendar-events",
  );
  const [name, setName] = useState(event?.name ?? "");
  const [scheduledAt, setScheduledAt] = useState(toLocalInput(event?.scheduledAt ?? null));
  const [releasedAt, setReleasedAt] = useState(toLocalInput(event?.releasedAt ?? null));
  const [previous, setPrevious] = useState(
    event?.previous == null ? "" : String(event.previous),
  );
  const [consensus, setConsensus] = useState(
    event?.consensus == null ? "" : String(event.consensus),
  );
  const [actual, setActual] = useState(event?.actual == null ? "" : String(event.actual));
  const [localError, setLocalError] = useState<string | null>(null);

  function close() {
    setLocalError(null);
    update.reset();
    onClose();
  }

  if (!event) return null;

  function submit() {
    const fields = parseEventFields(
      scheduledAt,
      releasedAt,
      previous,
      consensus,
      actual,
      setLocalError,
    );
    if (!fields) return;
    setLocalError(null);
    update.mutate(
      {
        name: name.trim(),
        scheduledAt: fields.scheduledAt,
        releasedAt: fields.releasedAt,
        previous: fields.previous,
        consensus: fields.consensus,
        actual: fields.actual,
      },
      { onSuccess: close },
    );
  }

  return (
    <AdminFormDialog
      open
      onClose={close}
      title="Edit calendar event"
      description="Saving keeps the stored revision count."
      pending={update.isPending}
      errorMessage={localError ?? adminErrorMessage(update.error)}
      onSubmit={submit}
    >
      <Field label="Name" htmlFor="edit-event-name">
        <Input
          id="edit-event-name"
          value={name}
          placeholder="US CPI"
          onChange={(event) => {
            setName(event.target.value);
            setLocalError(null);
          }}
        />
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Scheduled at" htmlFor="edit-event-scheduled">
          <Input
            id="edit-event-scheduled"
            type="datetime-local"
            value={scheduledAt}
            onChange={(event) => {
              setScheduledAt(event.target.value);
              setLocalError(null);
            }}
          />
        </Field>
        <Field label="Released at" htmlFor="edit-event-released" hint="Optional.">
          <Input
            id="edit-event-released"
            type="datetime-local"
            value={releasedAt}
            onChange={(event) => {
              setReleasedAt(event.target.value);
              setLocalError(null);
            }}
          />
        </Field>
        <Field label="Previous" htmlFor="edit-event-previous">
          <Input
            id="edit-event-previous"
            inputMode="decimal"
            value={previous}
            placeholder="250000"
            onChange={(event) => {
              setPrevious(event.target.value);
              setLocalError(null);
            }}
          />
        </Field>
        <Field label="Consensus" htmlFor="edit-event-consensus">
          <Input
            id="edit-event-consensus"
            inputMode="decimal"
            value={consensus}
            placeholder="255000"
            onChange={(event) => {
              setConsensus(event.target.value);
              setLocalError(null);
            }}
          />
        </Field>
        <Field label="Actual" htmlFor="edit-event-actual">
          <Input
            id="edit-event-actual"
            inputMode="decimal"
            value={actual}
            placeholder="261000"
            onChange={(event) => {
              setActual(event.target.value);
              setLocalError(null);
            }}
          />
        </Field>
      </div>
    </AdminFormDialog>
  );
}
