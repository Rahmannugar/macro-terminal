import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { AdminScreen } from "../../components/admin/admin-screen";
import { type AdminColumn, AdminTable } from "../../components/admin/admin-table";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { adminErrorMessage, adminRows, useAdminList, useAdminPost } from "../../lib/admin";
import { formatTimestamp } from "../../lib/format";

type FailedJobRow = {
  id: string;
  type: string;
  status: string;
  attempts: number;
  lastError: string | null;
  createdAt: string;
  updatedAt: string;
};

const jobsKey = ["admin", "jobs"];

export function FailedJobsScreen() {
  const list = useAdminList<FailedJobRow>({
    key: jobsKey,
    path: "/api/v1/admin/jobs/failed",
    rowsKey: "jobs",
  });
  const post = useAdminPost(jobsKey);
  const queryClient = useQueryClient();
  const [replayedId, setReplayedId] = useState<string | null>(null);

  function replay(row: FailedJobRow) {
    if (post.isPending) return;
    setReplayedId(row.id);
    post.mutate(`/api/v1/admin/jobs/${row.id}/replay`, {
      onSuccess: () => {
        queryClient.setQueriesData<{ pages: { rows: FailedJobRow[] }[] }>(
          { queryKey: jobsKey },
          (old) => {
            if (!old) return old;
            return {
              ...old,
              pages: old.pages.map((page) => ({
                ...page,
                rows: page.rows.filter((r) => r.id !== row.id),
              })),
            };
          },
        );
      },
      onSettled: () => setReplayedId(null),
    });
  }

  const columns: AdminColumn<FailedJobRow>[] = [
    {
      header: "Type",
      cell: (row) => <span className="font-mono text-xs font-medium">{row.type}</span>,
    },
    {
      header: "Status",
      cell: (row) => (
        <Badge tone={row.status === "failed" ? "danger" : "neutral"}>{row.status}</Badge>
      ),
    },
    {
      header: "Attempts",
      cell: (row) => <span className="tabular-nums">{row.attempts}</span>,
      className: "text-right",
    },
    {
      header: "Last error",
      cell: (row) => (
        <span
          className="block max-w-[380px] truncate text-xs text-muted-foreground"
          title={row.lastError ?? ""}
        >
          {row.lastError ?? "—"}
        </span>
      ),
    },
    {
      header: "Updated",
      cell: (row) => formatTimestamp(row.updatedAt),
      className: "text-muted-foreground",
    },
    {
      header: "",
      cell: (row) => (
        <Button
          variant="secondary"
          className="h-8 rounded-md px-3 text-xs font-medium"
          pending={replayedId === row.id && post.isPending}
          disabled={post.isPending && replayedId !== row.id}
          onClick={() => replay(row)}
        >
          Replay
        </Button>
      ),
      className: "w-28 text-right",
    },
  ];

  return (
    <AdminScreen
      title="Failed jobs"
      description="Work that exhausted its retries. Replaying resets a job so it runs again."
    >
      {post.isError ? (
        <p
          role="alert"
          className="mb-4 rounded-lg bg-red-500/10 px-3.5 py-2.5 text-sm text-red-600"
        >
          {adminErrorMessage(post.error)}
        </p>
      ) : null}
      <AdminTable
        columns={columns}
        rows={adminRows(list)}
        loading={list.isPending}
        loadingMore={list.isFetchingNextPage}
        hasNext={list.hasNextPage}
        error={list.error}
        onRetry={() => list.refetch()}
        onLoadMore={() => list.fetchNextPage()}
        emptyLabel="No failed jobs — the queues are drained."
      />
    </AdminScreen>
  );
}
