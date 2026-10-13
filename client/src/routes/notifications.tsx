import { BellIcon, CheckIcon } from "@phosphor-icons/react";
import { useNavigate } from "react-router";
import { AdminScreen } from "../components/admin/admin-screen";
import { Button } from "../components/ui/button";
import { Skeleton } from "../components/ui/skeleton";
import { adminErrorMessage, adminRows } from "../lib/admin";
import { formatTimestamp } from "../lib/format";
import {
  useMarkAllNotificationsRead,
  useMarkNotificationRead,
  useNotifications,
} from "../lib/notifications";

export function NotificationsRoute() {
  const notifications = useNotifications();
  const markRead = useMarkNotificationRead();
  const markAllRead = useMarkAllNotificationsRead();
  const navigate = useNavigate();

  const rows = adminRows(notifications);
  const hasUnread = rows.some((row) => !row.readAt);

  function openNotification(item: (typeof rows)[number]) {
    if (!item.readAt) markRead.mutate(item.id);
    if (item.subjectType === "article") navigate(`/articles/${item.subjectId}`);
    else navigate("/calendar");
  }

  return (
    <AdminScreen
      title="Notifications"
      description="Alerts from the pairs you follow: new articles and calendar events, newest first."
      actions={
        <Button
          variant="secondary"
          className="h-9! gap-1.5 px-3! text-xs"
          disabled={!hasUnread || markAllRead.isPending}
          pending={markAllRead.isPending}
          onClick={() => markAllRead.mutate()}
        >
          <CheckIcon aria-hidden="true" className="size-3.5" />
          Mark all read
        </Button>
      }
    >
      {notifications.isError ? (
        <p className="mt-3 text-sm text-muted-foreground">
          {adminErrorMessage(notifications.error)}
        </p>
      ) : notifications.isPending ? (
        <div className="mt-3 flex flex-col gap-2">
          {["row-1", "row-2", "row-3", "row-4", "row-5"].map((key) => (
            <Skeleton key={key} className="h-14 rounded-lg" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-lg border border-border bg-card px-4 py-10 text-center">
          <BellIcon aria-hidden="true" className="size-6 text-muted-foreground" />
          <p className="text-sm font-medium">No notifications yet</p>
          <p className="max-w-sm text-xs text-muted-foreground">
            Follow pairs on the watch list and keep the in-app channel switched on. New articles
            and calendar events for those pairs will show up here.
          </p>
        </div>
      ) : (
        <>
          <ul className="mt-1 flex flex-col gap-2">
            {rows.map((item) => {
              const unread = !item.readAt;
              return (
                <li key={item.id}>
                  <button
                    type="button"
                    className={`flex w-full items-center gap-3 rounded-lg border px-4 py-3 text-left transition-colors ${
                      unread
                        ? "border-border bg-card hover:bg-secondary"
                        : "border-border bg-card/50 opacity-75 hover:bg-card"
                    }`}
                    onClick={() => openNotification(item)}
                  >
                    <span
                      aria-hidden="true"
                      className={`size-2 shrink-0 rounded-full ${unread ? "bg-primary" : "bg-border"}`}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="flex items-baseline gap-2">
                        <span className="truncate text-sm font-medium">{item.title}</span>
                        <span className="shrink-0 text-xs font-semibold text-primary">
                          {item.pairSymbol}
                        </span>
                      </span>
                      <span className="mt-0.5 block text-xs text-muted-foreground">
                        {item.subjectType === "article" ? "New article" : "Calendar event"} ·{" "}
                        {formatTimestamp(item.createdAt)}
                      </span>
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
          {notifications.hasNextPage && !notifications.isFetchingNextPage ? (
            <div className="mt-3 flex justify-center">
              <Button
                variant="quiet"
                className="h-9! px-3! text-xs"
                onClick={() => notifications.fetchNextPage()}
              >
                Load more
              </Button>
            </div>
          ) : null}
          {!notifications.hasNextPage ? (
            <p className="mt-3 text-center text-xs text-muted-foreground">End of list</p>
          ) : null}
        </>
      )}
    </AdminScreen>
  );
}
