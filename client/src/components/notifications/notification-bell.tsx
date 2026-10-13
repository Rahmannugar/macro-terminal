import { BellIcon } from "@phosphor-icons/react";
import { Link } from "react-router";
import { useUnreadNotifications } from "../../lib/notifications";

export function NotificationBell() {
  const unread = useUnreadNotifications();
  const count = unread.data?.count ?? 0;
  const label = count > 0 ? `Notifications, ${count} unread` : "Notifications";

  return (
    <Link
      to="/notifications"
      aria-label={label}
      title={label}
      className="relative grid size-9 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
    >
      <BellIcon className="size-4" />
      {count > 0 ? (
        <span className="absolute -top-0.5 -right-0.5 grid min-w-4 place-items-center rounded-full bg-primary px-1 text-[10px] font-semibold leading-4 text-primary-foreground">
          {count > 9 ? "9+" : count}
        </span>
      ) : null}
    </Link>
  );
}
