import { useState } from "react";
import { AdminScreen } from "../../components/admin/admin-screen";
import { type AdminColumn, AdminTable } from "../../components/admin/admin-table";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Field } from "../../components/ui/field";
import { Select } from "../../components/ui/select";
import { useAccount } from "../../hooks/use-account";
import { adminErrorMessage, adminRows, useAdminList, useAdminPost } from "../../lib/admin";
import { formatTimestamp } from "../../lib/format";

type AdminUserRow = {
  id: string;
  username: string | null;
  role: string;
  status: string;
  createdAt: string;
  updatedAt: string;
};

const usersKey = ["admin", "users"];

export function UsersScreen() {
  const [role, setRole] = useState("");
  const [status, setStatus] = useState("");
  const list = useAdminList<AdminUserRow>({
    key: usersKey,
    path: "/api/v1/admin/users",
    rowsKey: "users",
    filters: { role, status },
  });
  const post = useAdminPost(usersKey);
  const account = useAccount();
  const actingId = account.data?.account.id;
  const pendingPath =
    post.isPending && typeof post.variables === "string" ? post.variables : null;

  function act(path: string) {
    if (post.isPending) return;
    post.mutate(path);
  }

  const columns: AdminColumn<AdminUserRow>[] = [
    {
      header: "Account",
      cell: (row) => (
        <div className="min-w-0">
          <p className="truncate font-medium">{row.username ?? "—"}</p>
        </div>
      ),
    },
    {
      header: "Role",
      cell: (row) => (
        <Badge tone={row.role === "admin" ? "primary" : "neutral"}>{row.role}</Badge>
      ),
    },
    {
      header: "Status",
      cell: (row) => (
        <Badge tone={row.status === "active" ? "success" : "danger"}>{row.status}</Badge>
      ),
    },
    {
      header: "Created",
      cell: (row) => formatTimestamp(row.createdAt),
      className: "text-muted-foreground",
    },
    {
      header: "",
      cell: (row) => {
        const isSelf = row.id === actingId;
        const busy = Boolean(pendingPath?.includes(row.id));
        return (
          <div className="flex justify-end gap-1.5">
            {row.role === "user" ? (
              <ActionButton
                label="Promote"
                busy={busy && pendingPath?.includes("/promote")}
                disabled={post.isPending}
                onClick={() => act(`/api/v1/admin/users/${row.id}/promote`)}
              />
            ) : null}
            {row.status === "active" && !isSelf ? (
              <ActionButton
                label="Suspend"
                busy={busy && pendingPath?.includes("/suspend")}
                disabled={post.isPending}
                onClick={() => act(`/api/v1/admin/users/${row.id}/suspend`)}
              />
            ) : null}
            {row.status === "suspended" ? (
              <ActionButton
                label="Reactivate"
                busy={busy && pendingPath?.includes("/reactivate")}
                disabled={post.isPending}
                onClick={() => act(`/api/v1/admin/users/${row.id}/reactivate`)}
              />
            ) : null}
          </div>
        );
      },
      className: "w-[260px] text-right",
    },
  ];

  return (
    <AdminScreen
      title="Users"
      description="Accounts, roles, and suspension. Suspending your own account is refused."
      actions={
        <div className="flex gap-2">
          <Field label="Role" htmlFor="users-role-filter">
            <Select
              id="users-role-filter"
              className="h-10 w-36 text-sm"
              value={role}
              onChange={(event) => setRole(event.target.value)}
            >
              <option value="">All roles</option>
              <option value="admin">admin</option>
              <option value="user">user</option>
            </Select>
          </Field>
          <Field label="Status" htmlFor="users-status-filter">
            <Select
              id="users-status-filter"
              className="h-10 w-40 text-sm"
              value={status}
              onChange={(event) => setStatus(event.target.value)}
            >
              <option value="">All statuses</option>
              <option value="active">active</option>
              <option value="suspended">suspended</option>
            </Select>
          </Field>
        </div>
      }
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
        emptyLabel="No accounts match these filters."
      />
    </AdminScreen>
  );
}

function ActionButton({
  label,
  busy,
  disabled,
  onClick,
}: {
  label: string;
  busy?: boolean;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      variant="quiet"
      className="h-8 rounded-md px-2.5 text-xs font-medium"
      pending={busy}
      disabled={disabled && !busy}
      onClick={onClick}
    >
      {label}
    </Button>
  );
}
