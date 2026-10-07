import { useState } from "react";
import { AdminFormDialog } from "../../components/admin/admin-form-dialog";
import { AdminScreen } from "../../components/admin/admin-screen";
import { type AdminColumn, AdminTable } from "../../components/admin/admin-table";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Field } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import type { EntityRow } from "../../hooks/use-references";
import { adminErrorMessage, adminRows, useAdminCreate, useAdminList } from "../../lib/admin";
import { formatTimestamp } from "../../lib/format";

const entitiesKey = ["admin", "entities"];

export function EntitiesScreen() {
  const list = useAdminList<EntityRow>({
    key: entitiesKey,
    path: "/api/v1/admin/entities",
    rowsKey: "entities",
  });
  const [createOpen, setCreateOpen] = useState(false);

  const columns: AdminColumn<EntityRow>[] = [
    {
      header: "Code",
      cell: (row) => <span className="font-mono text-xs font-semibold">{row.code}</span>,
    },
    { header: "Name", cell: (row) => row.name },
    { header: "Type", cell: (row) => <Badge>{row.type}</Badge> },
    {
      header: "Created",
      cell: (row) => formatTimestamp(row.createdAt),
      className: "text-muted-foreground",
    },
  ];

  return (
    <AdminScreen
      title="Entities"
      description="Reference entities — currencies, countries, and markets the terminal classifies against."
      actions={
        <Button className="h-10" onClick={() => setCreateOpen(true)}>
          Create entity
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
        emptyLabel="No entities yet."
      />
      <CreateEntityDialog open={createOpen} onClose={() => setCreateOpen(false)} />
    </AdminScreen>
  );
}

function CreateEntityDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const create = useAdminCreate<{ code: string; name: string; type: string }>(
    entitiesKey,
    "/api/v1/admin/entities",
  );
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [type, setType] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  function close() {
    setCode("");
    setName("");
    setType("");
    setLocalError(null);
    create.reset();
    onClose();
  }

  function submit() {
    if (!code.trim() || !name.trim() || !type.trim()) {
      setLocalError("Code, name, and type are required.");
      return;
    }
    setLocalError(null);
    create.mutate(
      { code: code.trim(), name: name.trim(), type: type.trim() },
      { onSuccess: close },
    );
  }

  return (
    <AdminFormDialog
      open={open}
      onClose={close}
      title="Create entity"
      description="The code must be unique — it is how every other resource references this entity."
      pending={create.isPending}
      errorMessage={localError ?? adminErrorMessage(create.error)}
      onSubmit={submit}
    >
      <Field label="Code" htmlFor="entity-code">
        <Input
          id="entity-code"
          autoFocus
          value={code}
          placeholder="EUR"
          onChange={(event) => {
            setCode(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        />
      </Field>
      <Field label="Name" htmlFor="entity-name">
        <Input
          id="entity-name"
          value={name}
          placeholder="Euro"
          onChange={(event) => {
            setName(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        />
      </Field>
      <Field
        label="Type"
        htmlFor="entity-type"
        hint="Free-form label such as currency, country, or index."
      >
        <Input
          id="entity-type"
          value={type}
          placeholder="currency"
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
