import { useState } from "react";
import { AdminFormDialog } from "../../components/admin/admin-form-dialog";
import { AdminScreen } from "../../components/admin/admin-screen";
import { type AdminColumn, AdminTable } from "../../components/admin/admin-table";
import { Button } from "../../components/ui/button";
import { Field } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { Select } from "../../components/ui/select";
import { labelFor, useEntityCodes, useEntityRows } from "../../hooks/use-references";
import { adminErrorMessage, adminRows, useAdminCreate, useAdminList } from "../../lib/admin";
import { formatTimestamp } from "../../lib/format";

type EntityPairRow = {
  id: string;
  symbol: string;
  baseEntityId: string;
  quoteEntityId: string;
  createdAt: string;
  updatedAt: string;
};

const pairsKey = ["admin", "entity-pairs"];

export function EntityPairsScreen() {
  const list = useAdminList<EntityPairRow>({
    key: pairsKey,
    path: "/api/v1/admin/entity-pairs",
    rowsKey: "entityPairs",
  });
  const [createOpen, setCreateOpen] = useState(false);
  const entityCodes = useEntityCodes();

  const columns: AdminColumn<EntityPairRow>[] = [
    {
      header: "Symbol",
      cell: (row) => <span className="font-mono text-xs font-semibold">{row.symbol}</span>,
    },
    { header: "Base", cell: (row) => labelFor(entityCodes, row.baseEntityId) },
    { header: "Quote", cell: (row) => labelFor(entityCodes, row.quoteEntityId) },
    {
      header: "Created",
      cell: (row) => formatTimestamp(row.createdAt),
      className: "text-muted-foreground",
    },
  ];

  return (
    <AdminScreen
      title="Entity pairs"
      description="Tradable pairs built from two existing entities under an explicit symbol."
      actions={
        <Button className="h-10" onClick={() => setCreateOpen(true)}>
          Create pair
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
        emptyLabel="No pairs yet."
      />
      <CreatePairDialog open={createOpen} onClose={() => setCreateOpen(false)} />
    </AdminScreen>
  );
}

function CreatePairDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const create = useAdminCreate<{ baseCode: string; quoteCode: string; symbol: string }>(
    pairsKey,
    "/api/v1/admin/entity-pairs",
  );
  const entities = useEntityRows();
  const [baseCode, setBaseCode] = useState("");
  const [quoteCode, setQuoteCode] = useState("");
  const [symbol, setSymbol] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  function close() {
    setBaseCode("");
    setQuoteCode("");
    setSymbol("");
    setLocalError(null);
    create.reset();
    onClose();
  }

  function submit() {
    if (!baseCode || !quoteCode || !symbol.trim()) {
      setLocalError("Both entities and the symbol are required.");
      return;
    }
    if (baseCode === quoteCode) {
      setLocalError("A pair needs two different entities.");
      return;
    }
    setLocalError(null);
    create.mutate(
      { baseCode, quoteCode, symbol: symbol.trim().toUpperCase() },
      { onSuccess: close },
    );
  }

  return (
    <AdminFormDialog
      open={open}
      onClose={close}
      title="Create entity pair"
      description="Pick the two sides of the pair and the symbol traders will see."
      pending={create.isPending}
      errorMessage={localError ?? adminErrorMessage(create.error)}
      onSubmit={submit}
    >
      <Field label="Base entity" htmlFor="pair-base">
        <Select
          id="pair-base"
          value={baseCode}
          onChange={(event) => {
            setBaseCode(event.target.value);
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
      <Field label="Quote entity" htmlFor="pair-quote">
        <Select
          id="pair-quote"
          value={quoteCode}
          onChange={(event) => {
            setQuoteCode(event.target.value);
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
      <Field label="Symbol" htmlFor="pair-symbol" hint="Stored uppercase, for example EURUSD.">
        <Input
          id="pair-symbol"
          value={symbol}
          placeholder="EURUSD"
          onChange={(event) => {
            setSymbol(event.target.value);
            setLocalError(null);
            create.reset();
          }}
        />
      </Field>
    </AdminFormDialog>
  );
}
