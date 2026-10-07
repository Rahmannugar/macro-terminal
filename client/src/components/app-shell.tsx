import { type ReactNode, useState } from "react";
import { Link, NavLink, useLocation, useNavigate } from "react-router";
import { useSignOut } from "../hooks/use-account";
import type { Account } from "../lib/auth";
import { Badge } from "./ui/badge";

type NavItem = { label: string; to: string; end?: boolean };
type NavSection = { label: string; adminOnly?: boolean; items: NavItem[] };

const navigation: NavSection[] = [
  { label: "Overview", items: [{ label: "Home", to: "/", end: true }] },
  {
    label: "Admin",
    adminOnly: true,
    items: [
      { label: "Users", to: "/admin/users" },
      { label: "Failed jobs", to: "/admin/jobs" },
    ],
  },
  {
    label: "Configuration",
    adminOnly: true,
    items: [
      { label: "Entities", to: "/admin/entities" },
      { label: "Entity pairs", to: "/admin/entity-pairs" },
      { label: "Indicators", to: "/admin/indicators" },
      { label: "Knowledge terms", to: "/admin/knowledge-terms" },
      { label: "Sources", to: "/admin/sources" },
      { label: "Source configurations", to: "/admin/source-configurations" },
      { label: "Calendar events", to: "/admin/calendar-events" },
    ],
  },
];

export function AppShell({ account, children }: { account: Account; children: ReactNode }) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const location = useLocation();
  const [lastPath, setLastPath] = useState(location.pathname);

  if (lastPath !== location.pathname) {
    setLastPath(location.pathname);
    setDrawerOpen(false);
  }

  const isAdmin = account.account.role === "admin";
  const sections = navigation.filter((section) => !section.adminOnly || isAdmin);

  return (
    <div className="min-h-svh bg-background">
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-60 flex-col border-r border-border bg-card md:flex">
        <SidebarBody account={account} sections={sections} />
      </aside>

      {drawerOpen ? (
        <div className="fixed inset-0 z-50 md:hidden">
          <div
            className="absolute inset-0 bg-black/45"
            aria-hidden="true"
            onClick={() => setDrawerOpen(false)}
          />
          <aside className="absolute inset-y-0 left-0 flex w-72 max-w-[85vw] flex-col border-r border-border bg-card shadow-2xl">
            <SidebarBody
              account={account}
              sections={sections}
              onClose={() => setDrawerOpen(false)}
            />
          </aside>
        </div>
      ) : null}

      <div className="flex min-h-svh flex-col md:pl-60">
        <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-border bg-background/90 px-4 backdrop-blur sm:px-6">
          <button
            type="button"
            aria-label="Open navigation"
            className="grid size-9 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground md:hidden"
            onClick={() => setDrawerOpen(true)}
          >
            <svg
              aria-hidden="true"
              viewBox="0 0 16 16"
              className="size-4"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
            >
              <path d="M2 4h12M2 8h12M2 12h12" strokeLinecap="round" />
            </svg>
          </button>
          <Link to="/" className="flex items-center gap-2 md:hidden">
            <span className="font-display text-[15px] font-semibold tracking-[-0.02em]">
              Macro Terminal
            </span>
          </Link>
          <div className="flex-1" />
          <AccountMenu account={account} />
        </header>
        <main className="mx-auto w-full max-w-[1160px] flex-1 px-4 py-7 sm:px-6 lg:px-8">
          {children}
        </main>
      </div>
    </div>
  );
}

function SidebarBody({
  account,
  sections,
  onClose,
}: {
  account: Account;
  sections: NavSection[];
  onClose?: () => void;
}) {
  const isAdmin = account.account.role === "admin";
  const identity = account.account.username ?? account.account.email;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-14 shrink-0 items-center gap-2.5 border-b border-border px-4">
        <Link to="/" className="flex min-w-0 items-center gap-2.5" onClick={onClose}>
          <img src="/macroterminal.png" alt="" width={26} height={26} className="rounded-lg" />
          <span className="truncate font-display text-[15px] font-semibold tracking-[-0.02em]">
            Macro Terminal
          </span>
        </Link>
        {onClose ? (
          <button
            type="button"
            aria-label="Close navigation"
            className="ml-auto grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            onClick={onClose}
          >
            <svg
              aria-hidden="true"
              viewBox="0 0 16 16"
              className="size-4"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
            >
              <path d="m4 4 8 8M12 4l-8 8" strokeLinecap="round" />
            </svg>
          </button>
        ) : null}
      </div>

      <nav className="min-h-0 flex-1 overflow-y-auto px-3 py-4">
        {sections.map((section) => (
          <div key={section.label} className="mb-5 last:mb-0">
            <p className="px-2.5 pb-1.5 text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">
              {section.label}
            </p>
            <div className="flex flex-col gap-0.5">
              {section.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.end}
                  onClick={onClose}
                  className={({ isActive }) =>
                    `flex h-9 items-center rounded-md px-2.5 text-[13px] font-medium transition-colors ${
                      isActive
                        ? "bg-secondary font-semibold text-foreground"
                        : "text-muted-foreground hover:bg-secondary hover:text-foreground"
                    }`
                  }
                >
                  {item.label}
                </NavLink>
              ))}
            </div>
          </div>
        ))}
      </nav>

      <div className="shrink-0 border-t border-border p-3">
        <div className="flex min-w-0 items-center gap-2.5 rounded-lg px-1.5 py-1.5">
          <span className="grid size-8 shrink-0 place-items-center rounded-full bg-primary/10 text-xs font-semibold text-primary">
            {identity.slice(0, 1).toUpperCase()}
          </span>
          <div className="min-w-0 flex-1">
            <p className="truncate text-[13px] font-medium">{identity}</p>
            <p className="truncate text-[11px] text-muted-foreground">
              {account.account.username ? account.account.email : "Signed in"}
            </p>
          </div>
          <Badge tone={isAdmin ? "primary" : "neutral"}>{account.account.role}</Badge>
        </div>
      </div>
    </div>
  );
}

function AccountMenu({ account }: { account: Account }) {
  const [open, setOpen] = useState(false);
  const signOut = useSignOut();
  const navigate = useNavigate();

  return (
    <div className="relative">
      <button
        type="button"
        aria-expanded={open}
        className="flex h-9 max-w-[260px] items-center gap-2 rounded-lg border border-border bg-card px-3 text-sm font-medium transition-colors hover:bg-secondary"
        onClick={() => setOpen((value) => !value)}
      >
        <span className="truncate">{account.account.username ?? account.account.email}</span>
        <svg
          aria-hidden="true"
          viewBox="0 0 16 16"
          className={`size-3.5 shrink-0 text-muted-foreground transition-transform ${open ? "rotate-180" : ""}`}
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <path d="m4 6 4 4 4-4" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      {open ? (
        <>
          <div
            className="fixed inset-0 z-40"
            aria-hidden="true"
            onClick={() => setOpen(false)}
          />
          <div className="absolute right-0 z-50 mt-1.5 w-64 rounded-xl border border-border bg-card p-1.5 shadow-xl">
            <div className="px-2.5 py-2">
              <p className="truncate text-sm font-medium">{account.account.email}</p>
              <p className="mt-0.5 text-xs text-muted-foreground">
                {account.account.username ? `${account.account.username} · ` : ""}
                {account.account.role}
              </p>
            </div>
            <div className="my-1 h-px bg-border" />
            <button
              type="button"
              className="w-full rounded-lg px-2.5 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              onClick={() =>
                signOut.mutate(undefined, {
                  onSettled: () => {
                    setOpen(false);
                    navigate("/sign-in", { replace: true });
                  },
                })
              }
            >
              {signOut.isPending ? "Signing out…" : "Sign out"}
            </button>
          </div>
        </>
      ) : null}
    </div>
  );
}
