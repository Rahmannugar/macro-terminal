import { PlusIcon, XIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { AdminFormDialog } from "../../components/admin/admin-form-dialog";
import { AdminScreen } from "../../components/admin/admin-screen";
import { type AdminColumn, AdminTable } from "../../components/admin/admin-table";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Field } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { Select } from "../../components/ui/select";
import { labelFor, useSourceNames, useSourceRows } from "../../hooks/use-references";
import {
  adminErrorMessage,
  adminRows,
  useAdminCreate,
  useAdminList,
  useAdminReplace,
} from "../../lib/admin";
import { formatTimestamp } from "../../lib/format";

type SourceConfigurationRow = {
  id: string;
  sourceId: string;
  type: string;
  config: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
  lastRunAt: string | null;
};

type SelectorRow = { id: string; key: string; value: string };

let selectorRowSeq = 0;
function newSelectorRow(key = "", value = ""): SelectorRow {
  selectorRowSeq += 1;
  return { id: `selector-${selectorRowSeq}`, key, value };
}

const configurationsKey = ["admin", "source-configurations"];

function selectorsFromConfig(config: Record<string, unknown>): SelectorRow[] {
  const raw = config.selectors;
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return [];
  return Object.entries(raw as Record<string, unknown>).map(([key, value]) =>
    newSelectorRow(key, typeof value === "string" ? value : String(value ?? "")),
  );
}

function selectorsToObject(rows: SelectorRow[]): Record<string, string> | undefined {
  const result: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (!key) continue;
    result[key] = row.value;
  }
  return Object.keys(result).length > 0 ? result : undefined;
}

function parseInterval(raw: string): number | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const value = Number(trimmed);
  if (!Number.isInteger(value) || value <= 0) return null;
  return value;
}

export function SourceConfigurationsScreen() {
  const list = useAdminList<SourceConfigurationRow>({
    key: configurationsKey,
    path: "/api/v1/admin/source-configurations",
    rowsKey: "sourceConfigurations",
  });
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<SourceConfigurationRow | null>(null);
  const sourceNames = useSourceNames();

  const columns: AdminColumn<SourceConfigurationRow>[] = [
    { header: "Source", cell: (row) => labelFor(sourceNames, row.sourceId) },
    { header: "Type", cell: (row) => <Badge>{row.type}</Badge> },
    {
      header: "Config",
      cell: (row) => (
        <code
          className="block max-w-[340px] truncate font-mono text-xs text-muted-foreground"
          title={JSON.stringify(row.config)}
        >
          {typeof row.config.url === "string" ? row.config.url : JSON.stringify(row.config)}
        </code>
      ),
    },
    {
      header: "Last run",
      cell: (row) => formatTimestamp(row.lastRunAt),
      className: "text-muted-foreground",
    },
    {
      header: "",
      cell: (row) => (
        <Button
          variant="quiet"
          className="h-8 rounded-md px-2.5 text-xs font-medium"
          onClick={() => setEditing(row)}
        >
          Edit
        </Button>
      ),
      className: "w-20 text-right",
    },
  ];

  return (
    <AdminScreen
      title="Source configurations"
      description="Set up how each source is fetched — feed URLs, intervals, and selectors."
      actions={
        <Button className="h-10" onClick={() => setCreateOpen(true)}>
          Create configuration
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
        emptyLabel="No configurations yet."
      />
      <CreateConfigurationDialog open={createOpen} onClose={() => setCreateOpen(false)} />
      <EditConfigurationDialog
        key={editing?.id ?? "edit"}
        configuration={editing}
        onClose={() => setEditing(null)}
      />
    </AdminScreen>
  );
}

function SelectorsEditor({
  rows,
  onChange,
}: {
  rows: SelectorRow[];
  onChange: (rows: SelectorRow[]) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <p className="text-sm font-medium">Selectors</p>
        <Button
          type="button"
          variant="quiet"
          className="h-8 gap-1.5 rounded-md px-2.5 text-xs font-medium"
          onClick={() => onChange([...rows, newSelectorRow()])}
        >
          <PlusIcon size={14} />
          Add selector
        </Button>
      </div>
      {rows.length === 0 ? (
        <p className="text-xs text-muted-foreground">No selectors on this configuration.</p>
      ) : null}
      {rows.map((row) => (
        <div key={row.id} className="flex items-center gap-2">
          <Input
            aria-label="Selector name"
            placeholder="title"
            value={row.key}
            className="h-10"
            onChange={(event) => {
              const next = rows.map((item) =>
                item.id === row.id ? { ...item, key: event.target.value } : item,
              );
              onChange(next);
            }}
          />
          <Input
            aria-label="Selector value"
            placeholder=".article-title"
            value={row.value}
            className="h-10"
            onChange={(event) => {
              const next = rows.map((item) =>
                item.id === row.id ? { ...item, value: event.target.value } : item,
              );
              onChange(next);
            }}
          />
          <button
            type="button"
            aria-label="Remove selector"
            className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition hover:text-foreground"
            onClick={() => onChange(rows.filter((item) => item.id !== row.id))}
          >
            <XIcon size={16} />
          </button>
        </div>
      ))}
    </div>
  );
}

function IntervalField({
  id,
  value,
  onChange,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <Field label="Fetch interval (seconds)" htmlFor={id} hint="Leave empty to use the default.">
      <Input
        id={id}
        type="number"
        min={1}
        step={1}
        inputMode="numeric"
        value={value}
        placeholder="e.g. 60"
        onChange={(event) => onChange(event.target.value)}
      />
    </Field>
  );
}

function CreateConfigurationDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const create = useAdminCreate<{
    sourceId: string;
    type: string;
    config: Record<string, unknown>;
  }>(configurationsKey, "/api/v1/admin/source-configurations");
  const sources = useSourceRows();
  const [sourceId, setSourceId] = useState("");
  const [type, setType] = useState("rss");
  const [url, setUrl] = useState("");
  const [interval, setInterval] = useState("");
  const [selectors, setSelectors] = useState<SelectorRow[]>([]);
  const [localError, setLocalError] = useState<string | null>(null);

  function close() {
    setSourceId("");
    setType("rss");
    setUrl("");
    setInterval("");
    setSelectors([]);
    setLocalError(null);
    create.reset();
    onClose();
  }

  function submit() {
    if (!sourceId) {
      setLocalError("Select a source.");
      return;
    }
    if (!url.trim()) {
      setLocalError("Enter a feed URL.");
      return;
    }
    const intervalSeconds = parseInterval(interval);
    if (interval.trim() && intervalSeconds === null) {
      setLocalError("Fetch interval must be a whole number of seconds.");
      return;
    }
    const config: Record<string, unknown> = { url: url.trim() };
    if (intervalSeconds !== null) config.min_interval_s = intervalSeconds;
    const selectorObject = selectorsToObject(selectors);
    if (selectorObject) config.selectors = selectorObject;
    setLocalError(null);
    create.mutate({ sourceId, type, config }, { onSuccess: close });
  }

  return (
    <AdminFormDialog
      open={open}
      onClose={close}
      title="Create configuration"
      description="Point a source at a feed URL or API endpoint."
      pending={create.isPending}
      errorMessage={localError ?? adminErrorMessage(create.error)}
      onSubmit={submit}
    >
      <Field label="Source" htmlFor="config-source">
        <Select
          id="config-source"
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
              {source.name} — {source.type}
            </option>
          ))}
        </Select>
      </Field>
      <Field label="Type" htmlFor="config-type">
        <Select
          id="config-type"
          value={type}
          onChange={(event) => {
            setType(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        >
          <option value="rss">rss</option>
          <option value="api">api</option>
          <option value="web">web</option>
        </Select>
      </Field>
      <Field label="Feed URL" htmlFor="config-url">
        <Input
          id="config-url"
          value={url}
          placeholder="https://example.com/feed.xml"
          spellCheck={false}
          onChange={(event) => {
            setUrl(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        />
      </Field>
      <IntervalField id="config-interval" value={interval} onChange={setInterval} />
      {type === "web" ? <SelectorsEditor rows={selectors} onChange={setSelectors} /> : null}
    </AdminFormDialog>
  );
}

function EditConfigurationDialog({
  configuration,
  onClose,
}: {
  configuration: SourceConfigurationRow | null;
  onClose: () => void;
}) {
  const replace = useAdminReplace<{ config: Record<string, unknown> }>(
    configurationsKey,
    configuration
      ? `/api/v1/admin/source-configurations/${configuration.id}`
      : "/api/v1/admin/source-configurations",
  );
  const [url, setUrl] = useState(
    configuration && typeof configuration.config.url === "string"
      ? configuration.config.url
      : "",
  );
  const [interval, setInterval] = useState(() => {
    const raw = configuration?.config.min_interval_s;
    return typeof raw === "number" ? String(raw) : "";
  });
  const [selectors, setSelectors] = useState<SelectorRow[]>(() =>
    selectorsFromConfig(configuration?.config ?? {}),
  );
  const [localError, setLocalError] = useState<string | null>(null);
  const showSelectors = configuration?.type === "web" || selectors.length > 0;

  function close() {
    setLocalError(null);
    replace.reset();
    onClose();
  }

  function submit() {
    if (!configuration) return;
    if (!url.trim()) {
      setLocalError("Enter a feed URL.");
      return;
    }
    const intervalSeconds = parseInterval(interval);
    if (interval.trim() && intervalSeconds === null) {
      setLocalError("Fetch interval must be a whole number of seconds.");
      return;
    }
    const next: Record<string, unknown> = { ...configuration.config, url: url.trim() };
    if (intervalSeconds !== null) {
      next.min_interval_s = intervalSeconds;
    } else {
      delete next.min_interval_s;
    }
    const selectorObject = selectorsToObject(selectors);
    if (selectorObject) {
      next.selectors = selectorObject;
    } else {
      delete next.selectors;
    }
    setLocalError(null);
    replace.mutate({ config: next }, { onSuccess: close });
  }

  if (!configuration) return null;

  return (
    <AdminFormDialog
      open
      onClose={close}
      title="Edit configuration"
      description="Update the feed URL, interval, or selectors for this source."
      submitLabel="Save"
      pending={replace.isPending}
      errorMessage={localError ?? adminErrorMessage(replace.error)}
      onSubmit={submit}
    >
      <Field label="Feed URL" htmlFor="config-edit-url">
        <Input
          id="config-edit-url"
          value={url}
          spellCheck={false}
          onChange={(event) => {
            setUrl(event.target.value);
            setLocalError(null);
            replace.reset();
          }}
        />
      </Field>
      <IntervalField
        id="config-edit-interval"
        value={interval}
        onChange={(value) => {
          setInterval(value);
          setLocalError(null);
          replace.reset();
        }}
      />
      {showSelectors ? (
        <SelectorsEditor
          rows={selectors}
          onChange={(rows) => {
            setSelectors(rows);
            setLocalError(null);
            replace.reset();
          }}
        />
      ) : null}
    </AdminFormDialog>
  );
}
