import type { ReactNode } from "react";
import { Button } from "../ui/button";
import { Skeleton } from "../ui/skeleton";

export type AdminColumn<T> = {
  header: string;
  cell: (row: T) => ReactNode;
  className?: string;
};

type AdminTableProps<T extends { id: string }> = {
  columns: AdminColumn<T>[];
  rows: T[];
  loading?: boolean;
  loadingMore?: boolean;
  hasNext?: boolean;
  error?: unknown;
  onRetry?: () => void;
  onLoadMore?: () => void;
  emptyLabel?: string;
};

export function AdminTable<T extends { id: string }>({
  columns,
  rows,
  loading,
  loadingMore,
  hasNext,
  error,
  onRetry,
  onLoadMore,
  emptyLabel = "Nothing here yet.",
}: AdminTableProps<T>) {
  if (loading) {
    return (
      <div className="flex flex-col gap-2">
        {["row-1", "row-2", "row-3", "row-4", "row-5"].map((key) => (
          <Skeleton key={key} className="h-12 rounded-xl" />
        ))}
      </div>
    );
  }
  if (error) {
    return (
      <div className="rounded-xl border border-border bg-card px-4 py-10 text-center">
        <p className="text-sm text-muted-foreground">
          {error instanceof Error ? error.message : "The list could not be loaded."}
        </p>
        {onRetry ? (
          <Button variant="secondary" className="mt-4 h-9 px-4 text-sm" onClick={onRetry}>
            Retry
          </Button>
        ) : null}
      </div>
    );
  }
  if (rows.length === 0) {
    return (
      <div className="rounded-xl border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
        {emptyLabel}
      </div>
    );
  }

  return (
    <div>
      <div className="overflow-x-auto rounded-xl border border-border bg-card">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              {columns.map((column) => (
                <th key={column.header} className="px-4 py-3 font-semibold">
                  {column.header}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr
                key={row.id}
                className="border-b border-border last:border-0 hover:bg-secondary/50"
              >
                {columns.map((column) => (
                  <td
                    key={column.header}
                    className={`px-4 py-3 align-middle ${column.className ?? ""}`}
                  >
                    {column.cell(row)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="mt-3 flex min-h-10 items-center justify-center">
        {hasNext ? (
          <Button
            variant="secondary"
            className="h-10 px-5 text-sm"
            pending={loadingMore}
            onClick={onLoadMore}
          >
            Load more
          </Button>
        ) : rows.length > 0 ? (
          <p className="text-xs text-muted-foreground">End of list</p>
        ) : null}
      </div>
    </div>
  );
}
