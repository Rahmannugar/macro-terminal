import { AppTopBar } from "../components/app-top-bar";
import { AuthenticatedBoundary } from "../components/auth/authenticated-boundary";

export function HomeRoute() {
  return (
    <AuthenticatedBoundary>
      {(account) => (
        <div className="min-h-svh bg-background">
          <AppTopBar account={account} />
          <main className="mx-auto flex max-w-[1160px] flex-col px-5 py-8 sm:px-8">
            <h1 className="font-display text-[28px] font-semibold tracking-[-0.04em]">
              Welcome back{account.account.username ? `, ${account.account.username}` : ""}
            </h1>
            <p className="mt-2 max-w-[560px] text-sm text-muted-foreground">
              Macro intelligence and fundamental research terminal. Terminal views arrive with
              the next slice.
            </p>
            <div className="mt-6 flex flex-wrap gap-3 text-sm">
              <span className="rounded-full bg-secondary px-3 py-1 font-medium">
                {account.account.role}
              </span>
              <span className="rounded-full bg-secondary px-3 py-1">
                {account.account.status}
              </span>
            </div>
          </main>
        </div>
      )}
    </AuthenticatedBoundary>
  );
}
