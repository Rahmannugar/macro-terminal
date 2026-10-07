import { useState } from "react";
import { AdminFormDialog } from "../../components/admin/admin-form-dialog";
import { AdminScreen } from "../../components/admin/admin-screen";
import { type AdminColumn, AdminTable } from "../../components/admin/admin-table";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Field } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import type { SourceRow } from "../../hooks/use-references";
import { adminErrorMessage, adminRows, useAdminCreate, useAdminList } from "../../lib/admin";
import { formatTimestamp } from "../../lib/format";

const sourcesKey = ["admin", "sources"];

export function SourcesScreen() {
  const list = useAdminList<SourceRow>({
    key: sourcesKey,
    path: "/api/v1/admin/sources",
    rowsKey: "sources",
  });
  const [createOpen, setCreateOpen] = useState(false);

  const columns: AdminColumn<SourceRow>[] = [
    { header: "Name", cell: (row) => <span className="font-medium">{row.name}</span> },
    { header: "Type", cell: (row) => <Badge>{row.type}</Badge> },
    {
      header: "Created",
      cell: (row) => formatTimestamp(row.createdAt),
      className: "text-muted-foreground",
    },
  ];

  return (
    <AdminScreen
      title="Sources"
      description="Logical sources — configurations, articles, and calendar events hang off these."
      actions={
        <Button className="h-10" onClick={() => setCreateOpen(true)}>
          Create source
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
        emptyLabel="No sources yet."
      />
      <CreateSourceDialog open={createOpen} onClose={() => setCreateOpen(false)} />
    </AdminScreen>
  );
}

function CreateSourceDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const create = useAdminCreate<{ name: string; type: string }>(
    sourcesKey,
    "/api/v1/admin/sources",
  );
  const [name, setName] = useState("");
  const [type, setType] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  function close() {
    setName("");
    setType("");
    setLocalError(null);
    create.reset();
    onClose();
  }

  function submit() {
    if (!name.trim() || !type.trim()) {
      setLocalError("Name and type are required.");
      return;
    }
    setLocalError(null);
    create.mutate({ name: name.trim(), type: type.trim() }, { onSuccess: close });
  }

  return (
    <AdminFormDialog
      open={open}
      onClose={close}
      title="Create source"
      description="The name must be unique. Configurations are added afterwards."
      pending={create.isPending}
      errorMessage={localError ?? adminErrorMessage(create.error)}
      onSubmit={submit}
    >
      <Field label="Name" htmlFor="source-name">
        <Input
          id="source-name"
          autoFocus
          value={name}
          placeholder="BLS"
          onChange={(event) => {
            setName(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        />
      </Field>
      <Field
        label="Type"
        htmlFor="source-type"
        hint="Free-form label such as official, news, or calendar."
      >
        <Input
          id="source-type"
          value={type}
          placeholder="official"
          onChange={(event) => {
            setType(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        />
      </Field>
    </AdminFormDialog>
  );
}
