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

const configurationsKey = ["admin", "source-configurations"];

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
  const [localError, setLocalError] = useState<string | null>(null);

  function close() {
    setSourceId("");
    setType("rss");
    setUrl("");
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
    setLocalError(null);
    create.mutate({ sourceId, type, config: { url: url.trim() } }, { onSuccess: close });
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
  const [localError, setLocalError] = useState<string | null>(null);

  function close() {
    setLocalError(null);
    replace.reset();
    onClose();
  }

  function submit() {
    if (!url.trim()) {
      setLocalError("Enter a feed URL.");
      return;
    }
    setLocalError(null);
    replace.mutate({ config: { url: url.trim() } }, { onSuccess: close });
  }

  if (!configuration) return null;

  return (
    <AdminFormDialog
      open
      onClose={close}
      title="Edit configuration"
      description="Update the feed URL or API endpoint for this source."
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
    </AdminFormDialog>
  );
}
