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
  useEntityCodes,
  useEntityRows,
  useIndicatorNames,
  useIndicatorRows,
} from "../../hooks/use-references";
import { adminErrorMessage, adminRows, useAdminCreate, useAdminList } from "../../lib/admin";
import { formatTimestamp } from "../../lib/format";

type KnowledgeTermRow = {
  id: string;
  name: string;
  type: string;
  entityId: string | null;
  indicatorId: string | null;
  createdAt: string;
  updatedAt: string;
};

const termsKey = ["admin", "knowledge-terms"];

export function KnowledgeTermsScreen() {
  const list = useAdminList<KnowledgeTermRow>({
    key: termsKey,
    path: "/api/v1/admin/knowledge-terms",
    rowsKey: "knowledgeTerms",
  });
  const [createOpen, setCreateOpen] = useState(false);
  const entityCodes = useEntityCodes();
  const indicatorNames = useIndicatorNames();

  const columns: AdminColumn<KnowledgeTermRow>[] = [
    { header: "Term", cell: (row) => <span className="font-medium">{row.name}</span> },
    { header: "Type", cell: (row) => <Badge>{row.type.replaceAll("_", " ")}</Badge> },
    {
      header: "Entity",
      cell: (row) =>
        row.entityId ? (
          labelFor(entityCodes, row.entityId)
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
    {
      header: "Indicator",
      cell: (row) =>
        row.indicatorId ? (
          labelFor(indicatorNames, row.indicatorId)
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
    {
      header: "Created",
      cell: (row) => formatTimestamp(row.createdAt),
      className: "text-muted-foreground",
    },
  ];

  return (
    <AdminScreen
      title="Knowledge terms"
      description="Classification phrases linked to an entity, an indicator, or both — how titles map onto the terminal."
      actions={
        <Button className="h-10" onClick={() => setCreateOpen(true)}>
          Create term
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
        emptyLabel="No knowledge terms yet."
      />
      <CreateTermDialog open={createOpen} onClose={() => setCreateOpen(false)} />
    </AdminScreen>
  );
}

function CreateTermDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const create = useAdminCreate<{
    name: string;
    type: string;
    entityCode?: string;
    indicatorId?: string;
  }>(termsKey, "/api/v1/admin/knowledge-terms");
  const entities = useEntityRows();
  const indicators = useIndicatorRows();
  const [name, setName] = useState("");
  const [type, setType] = useState("");
  const [entityCode, setEntityCode] = useState("");
  const [indicatorId, setIndicatorId] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  function close() {
    setName("");
    setType("");
    setEntityCode("");
    setIndicatorId("");
    setLocalError(null);
    create.reset();
    onClose();
  }

  function submit() {
    if (!name.trim() || !type.trim()) {
      setLocalError("Name and type are required.");
      return;
    }
    if (!entityCode && !indicatorId) {
      setLocalError("Link the term to an entity, an indicator, or both.");
      return;
    }
    setLocalError(null);
    create.mutate(
      {
        name: name.trim(),
        type: type.trim(),
        ...(entityCode ? { entityCode } : {}),
        ...(indicatorId ? { indicatorId } : {}),
      },
      { onSuccess: close },
    );
  }

  return (
    <AdminFormDialog
      open={open}
      onClose={close}
      title="Create knowledge term"
      description="An indicator link carries the indicator's entity automatically."
      pending={create.isPending}
      errorMessage={localError ?? adminErrorMessage(create.error)}
      onSubmit={submit}
    >
      <Field label="Term" htmlFor="term-name">
        <Input
          id="term-name"
          autoFocus
          value={name}
          placeholder="nonfarm payrolls"
          onChange={(event) => {
            setName(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        />
      </Field>
      <Field
        label="Type"
        htmlFor="term-type"
        hint="For example indicator_alias, institution, person, or topic."
      >
        <Input
          id="term-type"
          value={type}
          placeholder="indicator_alias"
          onChange={(event) => {
            setType(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        />
      </Field>
      <Field
        label="Entity"
        htmlFor="term-entity"
        hint="Optional when an indicator is selected."
      >
        <Select
          id="term-entity"
          value={entityCode}
          onChange={(event) => {
            setEntityCode(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        >
          <option value="">None</option>
          {entities.map((entity) => (
            <option key={entity.id} value={entity.code}>
              {entity.code} — {entity.name}
            </option>
          ))}
        </Select>
      </Field>
      <Field
        label="Indicator"
        htmlFor="term-indicator"
        hint="Optional when an entity is selected."
      >
        <Select
          id="term-indicator"
          value={indicatorId}
          onChange={(event) => {
            setIndicatorId(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        >
          <option value="">None</option>
          {indicators.map((indicator) => (
            <option key={indicator.id} value={indicator.id}>
              {indicator.name}
            </option>
          ))}
        </Select>
      </Field>
    </AdminFormDialog>
  );
}
