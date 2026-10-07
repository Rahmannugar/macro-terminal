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
  type IndicatorRow,
  labelFor,
  useEntityCodes,
  useEntityRows,
} from "../../hooks/use-references";
import { adminErrorMessage, adminRows, useAdminCreate, useAdminList } from "../../lib/admin";
import { formatTimestamp } from "../../lib/format";

const indicatorsKey = ["admin", "indicators"];

export function IndicatorsScreen() {
  const list = useAdminList<IndicatorRow>({
    key: indicatorsKey,
    path: "/api/v1/admin/indicators",
    rowsKey: "indicators",
  });
  const [createOpen, setCreateOpen] = useState(false);
  const entityCodes = useEntityCodes();

  const columns: AdminColumn<IndicatorRow>[] = [
    { header: "Name", cell: (row) => <span className="font-medium">{row.name}</span> },
    { header: "Type", cell: (row) => <Badge>{row.type}</Badge> },
    { header: "Entity", cell: (row) => labelFor(entityCodes, row.entityId) },
    {
      header: "Created",
      cell: (row) => formatTimestamp(row.createdAt),
      className: "text-muted-foreground",
    },
  ];

  return (
    <AdminScreen
      title="Economic indicators"
      description="Indicators belong to one entity and carry the series the calendar tracks."
      actions={
        <Button className="h-10" onClick={() => setCreateOpen(true)}>
          Create indicator
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
        emptyLabel="No indicators yet."
      />
      <CreateIndicatorDialog open={createOpen} onClose={() => setCreateOpen(false)} />
    </AdminScreen>
  );
}

function CreateIndicatorDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const create = useAdminCreate<{ name: string; type: string; entityCode: string }>(
    indicatorsKey,
    "/api/v1/admin/indicators",
  );
  const entities = useEntityRows();
  const [name, setName] = useState("");
  const [type, setType] = useState("");
  const [entityCode, setEntityCode] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  function close() {
    setName("");
    setType("");
    setEntityCode("");
    setLocalError(null);
    create.reset();
    onClose();
  }

  function submit() {
    if (!name.trim() || !type.trim() || !entityCode) {
      setLocalError("Name, type, and entity are required.");
      return;
    }
    setLocalError(null);
    create.mutate({ name: name.trim(), type: type.trim(), entityCode }, { onSuccess: close });
  }

  return (
    <AdminFormDialog
      open={open}
      onClose={close}
      title="Create indicator"
      description="The name must be unique within its entity."
      pending={create.isPending}
      errorMessage={localError ?? adminErrorMessage(create.error)}
      onSubmit={submit}
    >
      <Field label="Name" htmlFor="indicator-name">
        <Input
          id="indicator-name"
          autoFocus
          value={name}
          placeholder="Nonfarm Payrolls"
          onChange={(event) => {
            setName(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        />
      </Field>
      <Field
        label="Type"
        htmlFor="indicator-type"
        hint="Free-form label such as labor, prices, or policy."
      >
        <Input
          id="indicator-type"
          value={type}
          placeholder="labor"
          onChange={(event) => {
            setType(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        />
      </Field>
      <Field label="Entity" htmlFor="indicator-entity">
        <Select
          id="indicator-entity"
          value={entityCode}
          onChange={(event) => {
            setEntityCode(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        >
          <option value="">Select an entity</option>
          {entities.map((entity) => (
            <option key={entity.id} value={entity.code}>
              {entity.code} — {entity.name}
            </option>
          ))}
        </Select>
      </Field>
    </AdminFormDialog>
  );
}
