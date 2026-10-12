import {
  BookOpenIcon,
  CalendarIcon,
  ClipboardTextIcon,
  EyeIcon,
  GaugeIcon,
  HouseIcon,
  LinkSimpleIcon,
  ListIcon,
  MagnifyingGlassIcon,
  RssIcon,
  SidebarSimpleIcon,
  SlidersHorizontalIcon,
  StackIcon,
  TrendUpIcon,
  UserIcon,
  UsersIcon,
  WarningIcon,
  XIcon,
} from "@phosphor-icons/react";
import { type ComponentType, type ReactNode, useState } from "react";
import { Link, NavLink, useLocation, useNavigate } from "react-router";
import { useAccount, useSignOut, useUpdateUsername } from "../hooks/use-account";
import type { Account } from "../lib/auth";
import { ThemeToggle } from "./theme-toggle";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Field } from "./ui/field";
import { Input } from "./ui/input";

type NavItem = {
  label: string;
  to: string;
  end?: boolean;
  icon: ComponentType<{ className?: string }>;
};
type NavSection = { label?: string; adminOnly?: boolean; items: NavItem[] };

const navigation: NavSection[] = [
  {
    items: [
      { label: "Home", to: "/", end: true, icon: HouseIcon },
      { label: "Search", to: "/search", icon: MagnifyingGlassIcon },
      { label: "Watch list", to: "/watchlist", icon: EyeIcon },
      { label: "Calendar", to: "/calendar", icon: CalendarIcon },
      { label: "Market", to: "/market", icon: TrendUpIcon },
    ],
  },
  {
    label: "Admin",
    adminOnly: true,
    items: [
      { label: "Users", to: "/admin/users", icon: UsersIcon },
      { label: "Failed jobs", to: "/admin/jobs", icon: WarningIcon },
    ],
  },
  {
    label: "Configuration",
    adminOnly: true,
    items: [
      { label: "Entities", to: "/admin/entities", icon: StackIcon },
      { label: "Entity pairs", to: "/admin/entity-pairs", icon: LinkSimpleIcon },
      { label: "Indicators", to: "/admin/indicators", icon: GaugeIcon },
      { label: "Knowledge terms", to: "/admin/knowledge-terms", icon: BookOpenIcon },
      { label: "Sources", to: "/admin/sources", icon: RssIcon },
      {
        label: "Source configurations",
        to: "/admin/source-configurations",
        icon: SlidersHorizontalIcon,
      },
      { label: "Calendar events", to: "/admin/calendar-events", icon: ClipboardTextIcon },
    ],
  },
];

const sidebarStorageKey = "macro-terminal.sidebar";

function loadSidebarCollapsed(): boolean {
  try {
    return localStorage.getItem(sidebarStorageKey) === "collapsed";
  } catch {
    return false;
  }
}

export function AppShell({ account, children }: { account: Account; children: ReactNode }) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(loadSidebarCollapsed);
  const location = useLocation();
  const [lastPath, setLastPath] = useState(location.pathname);

  if (lastPath !== location.pathname) {
    setLastPath(location.pathname);
    setDrawerOpen(false);
  }

  function toggleCollapsed() {
    setCollapsed((value) => {
      const next = !value;
      try {
        localStorage.setItem(sidebarStorageKey, next ? "collapsed" : "expanded");
      } catch {
        // Storage unavailable; the toggle still applies for this session.
      }
      return next;
    });
  }

  const isAdmin = account.account.role === "admin" || account.account.role === "super_admin";
  const sections = navigation.filter((section) => !section.adminOnly || isAdmin);

  return (
    <div className="min-h-svh bg-background">
      <aside
        className={`fixed inset-y-0 left-0 z-30 hidden flex-col border-r border-border bg-background md:flex ${
          collapsed ? "w-14" : "w-60"
        }`}
      >
        <SidebarBody sections={sections} collapsed={collapsed} />
      </aside>

      {drawerOpen ? (
        <div className="fixed inset-0 z-50 md:hidden">
          <div
            className="absolute inset-0 bg-black/45"
            aria-hidden="true"
            onClick={() => setDrawerOpen(false)}
          />
          <aside className="absolute inset-y-0 left-0 flex w-72 max-w-[85vw] flex-col border-r border-border bg-background shadow-2xl">
            <SidebarBody
              sections={sections}
              collapsed={false}
              onClose={() => setDrawerOpen(false)}
            />
          </aside>
        </div>
      ) : null}

      <div className={`flex min-h-svh flex-col ${collapsed ? "md:pl-14" : "md:pl-60"}`}>
        <header className="sticky top-0 z-20 flex h-14 items-center gap-3 bg-background/90 px-4 backdrop-blur-xl sm:px-6">
          <button
            type="button"
            aria-label={collapsed ? "Expand navigation" : "Collapse navigation"}
            title={collapsed ? "Expand navigation" : "Collapse navigation"}
            className="hidden size-8 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground md:grid"
            onClick={toggleCollapsed}
          >
            <SidebarSimpleIcon className="size-[18px]" />
          </button>
          <button
            type="button"
            aria-label="Open navigation"
            className="grid size-9 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground md:hidden"
            onClick={() => setDrawerOpen(true)}
          >
            <ListIcon className="size-4" />
          </button>
          <Link to="/" className="flex items-center gap-2 md:hidden">
            <span className="font-display text-[15px] font-semibold tracking-[-0.02em]">
              Macro Terminal
            </span>
          </Link>
          <div className="flex-1" />
          <ThemeToggle />
          <AccountMenu />
        </header>
        <main className="mx-auto w-full max-w-[1160px] flex-1 px-4 py-7 sm:px-6 lg:px-8">
          {children}
        </main>
      </div>
    </div>
  );
}

function SidebarBody({
  sections,
  collapsed,
  onClose,
}: {
  sections: NavSection[];
  collapsed: boolean;
  onClose?: () => void;
}) {
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        className={`flex shrink-0 items-center py-3 ${
          collapsed ? "justify-center" : "gap-2.5 px-4"
        }`}
      >
        <Link
          to="/"
          className={`flex min-w-0 items-center ${collapsed ? "" : "gap-2.5"}`}
          onClick={onClose}
          title={collapsed ? "Macro Terminal" : undefined}
        >
          <img
            src="/macroterminal.png"
            alt=""
            width={26}
            height={26}
            className="shrink-0 rounded-lg"
          />
          {collapsed ? null : (
            <span className="truncate font-display text-[15px] font-semibold tracking-[-0.02em]">
              Macro Terminal
            </span>
          )}
        </Link>
        {onClose ? (
          <button
            type="button"
            aria-label="Close navigation"
            className="ml-auto grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            onClick={onClose}
          >
            <XIcon className="size-4" />
          </button>
        ) : null}
      </div>

      <nav className="min-h-0 flex-1 overflow-y-auto px-3 pb-4 pt-1">
        {sections.map((section, index) => (
          <div key={section.label ?? index} className="mb-5 last:mb-0">
            {section.label && !collapsed ? (
              <p className="px-2.5 pb-1.5 text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                {section.label}
              </p>
            ) : null}
            <div className="flex flex-col gap-0.5">
              {section.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.end}
                  onClick={onClose}
                  title={collapsed ? item.label : undefined}
                  className={({ isActive }) =>
                    `flex h-9 items-center rounded-md text-[13px] font-medium transition-colors ${
                      collapsed ? "justify-center px-0" : "gap-2.5 px-2.5"
                    } ${
                      isActive
                        ? "bg-secondary font-semibold text-foreground"
                        : "text-muted-foreground hover:bg-secondary hover:text-foreground"
                    }`
                  }
                >
                  <item.icon className="size-[18px] shrink-0" />
                  {collapsed ? null : <span className="truncate">{item.label}</span>}
                </NavLink>
              ))}
            </div>
          </div>
        ))}
      </nav>
    </div>
  );
}

function AccountMenu() {
  const [open, setOpen] = useState(false);
  const [profileOpen, setProfileOpen] = useState(false);
  const account = useAccount();
  const signOut = useSignOut();
  const navigate = useNavigate();

  const data = account.data;
  if (!data) return null;

  return (
    <>
      <div className="relative">
        <button
          type="button"
          aria-label="Account menu"
          aria-expanded={open}
          className="grid size-9 place-items-center rounded-full bg-primary text-primary-foreground transition-colors hover:bg-primary/90"
          onClick={() => setOpen((value) => !value)}
        >
          <UserIcon className="size-4" />
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
                <p className="truncate text-sm font-medium">{data.account.email}</p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  {[
                    data.account.username,
                    data.account.role === "admin" || data.account.role === "super_admin"
                      ? data.account.role
                      : null,
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </p>
              </div>
              <div className="my-1 h-px bg-border" />
              <button
                type="button"
                className="w-full rounded-lg px-2.5 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                onClick={() => {
                  setOpen(false);
                  setProfileOpen(true);
                }}
              >
                Edit profile
              </button>
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
      <EditProfileDialog
        open={profileOpen}
        onClose={() => setProfileOpen(false)}
        initialUsername={data.account.username ?? ""}
        email={data.account.email}
      />
    </>
  );
}

function EditProfileDialog({
  open,
  onClose,
  initialUsername,
  email,
}: {
  open: boolean;
  onClose: () => void;
  initialUsername: string;
  email: string;
}) {
  const [username, setUsername] = useState(initialUsername);
  const update = useUpdateUsername();

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Edit profile"
      description="Your display name shown across Macro Terminal."
    >
      <form
        className="mt-4 flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          update.mutate({ username: username.trim() }, { onSuccess: () => onClose() });
        }}
      >
        <Field label="Username" htmlFor="edit-profile-username">
          <Input
            id="edit-profile-username"
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            placeholder="macrofan"
            maxLength={64}
          />
        </Field>
        <Field label="Email" htmlFor="edit-profile-email">
          <Input id="edit-profile-email" value={email} disabled />
        </Field>
        {update.isError ? (
          <p className="text-sm text-red-600">Could not save. Try again.</p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" pending={update.isPending}>
            Save
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
