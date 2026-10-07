import { createRail, RailBoundary, requireAuth, requireRole } from "authrail";
import type { ReactNode } from "react";
import { useNavigate } from "react-router";
import { useAccount, useSignOut } from "../../hooks/use-account";
import type { Account } from "../../lib/auth";
import { Button } from "../ui/button";

type RailContext = { user: { role: string } | null };

const authenticatedRail = createRail<RailContext>("authenticated", [requireAuth("/sign-in")]);
const adminRail = createRail<RailContext>("admin", [requireRole("admin")]);

export function AuthenticatedBoundary({
  children,
}: {
  children: (account: Account) => ReactNode;
}) {
  const account = useAccount();
  const navigate = useNavigate();

  if (account.isPending) return <AccountLoading />;
  if (account.isError) {
    if (account.error.status === 401) {
      return (
        <RailBoundary
          rail={authenticatedRail}
          context={{ user: null }}
          fallback={null}
          onRedirect={(to) => navigate(to, { replace: true })}
        >
          {null}
        </RailBoundary>
      );
    }
    if (account.error.status === 403 && account.error.code === "account_suspended") {
      return <SuspendedScreen />;
    }
    return <AccountError onRetry={() => account.refetch()} />;
  }
  if (!account.data) return <AccountLoading />;

  return (
    <RailBoundary
      rail={authenticatedRail}
      context={{ user: { role: account.data.account.role } }}
      fallback={<AccountLoading />}
      onRedirect={(to) => navigate(to, { replace: true })}
    >
      {children(account.data)}
    </RailBoundary>
  );
}

export function AdminBoundary({ children }: { children: (account: Account) => ReactNode }) {
  const account = useAccount();

  if (account.isPending) return <AccountLoading />;
  if (account.isError) return null;
  if (!account.data) return <AccountLoading />;

  return (
    <RailBoundary
      rail={adminRail}
      context={{ user: { role: account.data.account.role } }}
      fallback={<AccountLoading />}
      denied={<AdminsOnlyScreen />}
    >
      {children(account.data)}
    </RailBoundary>
  );
}

export function AccountLoading() {
  return (
    <div className="flex min-h-svh items-center justify-center bg-background">
      <p className="text-sm text-muted-foreground">Loading…</p>
    </div>
  );
}

export function AccountError({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex min-h-svh items-center justify-center bg-background px-5">
      <div className="w-full max-w-[400px] rounded-xl border border-border bg-card p-7">
        <h1 className="font-display text-[22px] font-semibold tracking-[-0.03em]">
          We could not load your account
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Macro Terminal will try again when you retry.
        </p>
        <Button variant="secondary" className="mt-5 w-full" onClick={onRetry}>
          Retry
        </Button>
      </div>
    </div>
  );
}

export function SuspendedScreen() {
  const signOut = useSignOut();
  const navigate = useNavigate();

  return (
    <div className="flex min-h-svh items-center justify-center bg-background px-5">
      <div className="w-full max-w-[400px] rounded-xl border border-border bg-card p-7">
        <img src="/macroterminal.png" alt="" width={44} height={44} className="rounded-xl" />
        <h1 className="mt-4 font-display text-[22px] font-semibold tracking-[-0.03em]">
          Account suspended
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          This account has been suspended. Contact an administrator if you believe this is a
          mistake.
        </p>
        <Button
          variant="secondary"
          className="mt-5 w-full"
          pending={signOut.isPending}
          onClick={() =>
            signOut.mutate(undefined, {
              onSettled: () => navigate("/sign-in", { replace: true }),
            })
          }
        >
          Sign out
        </Button>
      </div>
    </div>
  );
}

function AdminsOnlyScreen() {
  return (
    <div className="mx-auto my-16 w-full max-w-[440px] rounded-xl border border-border bg-card p-7">
      <h1 className="font-display text-[22px] font-semibold tracking-[-0.03em]">Admins only</h1>
      <p className="mt-2 text-sm text-muted-foreground">
        Your account does not have access to this area of Macro Terminal.
      </p>
    </div>
  );
}
