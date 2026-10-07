import { Link } from "react-router";
import { useAccount } from "../hooks/use-account";

export function HomeRoute() {
  const account = useAccount();
  const data = account.data;
  if (!data) return null;

  const isAdmin = data.account.role === "admin";

  return (
    <div>
      <h1 className="font-display text-[28px] font-semibold tracking-[-0.04em]">
        Welcome back{data.account.username ? `, ${data.account.username}` : ""}
      </h1>
      <p className="mt-2 max-w-[560px] text-sm text-muted-foreground">
        Macro intelligence and fundamental research terminal. Terminal views arrive with the
        next slice.
      </p>
      <div className="mt-6 flex flex-wrap gap-3 text-sm">
        <span className="rounded-full bg-secondary px-3 py-1 font-medium">
          {data.account.role}
        </span>
        <span className="rounded-full bg-secondary px-3 py-1">{data.account.status}</span>
      </div>
      {isAdmin ? (
        <div className="mt-10">
          <h2 className="text-sm font-semibold">Admin</h2>
          <div className="mt-3 grid max-w-[720px] gap-3 sm:grid-cols-2">
            <AdminShortcut
              to="/admin/users"
              title="Users"
              description="Accounts, roles, suspension."
            />
            <AdminShortcut
              to="/admin/jobs"
              title="Failed jobs"
              description="Retry exhausted work."
            />
            <AdminShortcut
              to="/admin/entities"
              title="Configuration"
              description="Entities, pairs, indicators, terms."
            />
            <AdminShortcut
              to="/admin/source-configurations"
              title="Sources"
              description="Configurations and fetch payloads."
            />
          </div>
        </div>
      ) : null}
    </div>
  );
}

function AdminShortcut({
  to,
  title,
  description,
}: {
  to: string;
  title: string;
  description: string;
}) {
  return (
    <Link
      to={to}
      className="rounded-xl border border-border bg-card p-4 transition-colors hover:border-primary/40 hover:bg-secondary/60"
    >
      <p className="text-sm font-semibold">{title}</p>
      <p className="mt-1 text-xs text-muted-foreground">{description}</p>
    </Link>
  );
}
