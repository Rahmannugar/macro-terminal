import { Link, useNavigate } from "react-router";
import { useSignOut } from "../hooks/use-account";
import type { Account } from "../lib/auth";
import { Button } from "./ui/button";

export function AppTopBar({ account }: { account: Account }) {
  const signOut = useSignOut();
  const navigate = useNavigate();

  return (
    <header className="sticky top-0 z-40 flex h-14 items-center gap-3 border-b border-border bg-background/90 px-5 backdrop-blur">
      <Link to="/" className="flex items-center gap-2.5">
        <img src="/macroterminal.png" alt="" width={28} height={28} className="rounded-lg" />
        <span className="font-display text-[15px] font-semibold tracking-[-0.02em]">
          Macro Terminal
        </span>
      </Link>
      <span className="ml-auto hidden max-w-[260px] truncate text-sm text-muted-foreground sm:block">
        {account.account.email}
      </span>
      <Button
        variant="quiet"
        className="h-9 px-3 text-xs font-medium"
        pending={signOut.isPending}
        onClick={() =>
          signOut.mutate(undefined, {
            onSettled: () => navigate("/sign-in", { replace: true }),
          })
        }
      >
        Sign out
      </Button>
    </header>
  );
}
