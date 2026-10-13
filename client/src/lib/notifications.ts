import { type QueryKey, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAdminList } from "./admin";
import { type APIError, apiRequest } from "./api";

export type NotificationItem = {
  id: string;
  type: string;
  subjectType: string;
  subjectId: string;
  title: string;
  pairSymbol: string;
  readAt?: string | null;
  createdAt: string;
};

export type NotificationChannel = {
  channel: string;
  endpoint?: string | null;
};

export type NotificationSettings = {
  inApp: boolean;
  channels: NotificationChannel[];
  alertPairIds: string[];
};

export const notificationsKey: QueryKey = ["notifications"];
export const notificationUnreadKey: QueryKey = ["notifications", "unread"];
export const notificationSettingsKey: QueryKey = ["notifications", "settings"];

export function useNotifications() {
  return useAdminList<NotificationItem>({
    key: notificationsKey,
    path: "/api/v1/notifications",
    rowsKey: "notifications",
  });
}

export function useUnreadNotifications() {
  return useQuery<{ count: number }, APIError>({
    queryKey: notificationUnreadKey,
    queryFn: async () =>
      (await apiRequest("/api/v1/notifications/unread-count")) as { count: number },
    refetchInterval: 45_000,
    staleTime: 30_000,
    retry: false,
  });
}

export function useNotificationSettings() {
  return useQuery<NotificationSettings, APIError>({
    queryKey: notificationSettingsKey,
    queryFn: async () =>
      (await apiRequest("/api/v1/notification-settings")) as NotificationSettings,
    staleTime: 15_000,
    retry: false,
  });
}

function useInvalidateNotifications() {
  const queryClient = useQueryClient();
  return () => {
    queryClient.invalidateQueries({ queryKey: notificationsKey });
    queryClient.invalidateQueries({ queryKey: notificationUnreadKey });
  };
}

export function useMarkNotificationRead() {
  const invalidate = useInvalidateNotifications();
  return useMutation({
    mutationFn: (notificationId: string) =>
      apiRequest(`/api/v1/notifications/${notificationId}/read`, { method: "POST" }),
    onSettled: invalidate,
  });
}

export function useMarkAllNotificationsRead() {
  const invalidate = useInvalidateNotifications();
  return useMutation({
    mutationFn: () => apiRequest("/api/v1/notifications/read-all", { method: "POST" }),
    onSettled: invalidate,
  });
}

export function useSetInAppChannel() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (enabled: boolean) =>
      apiRequest("/api/v1/notification-channels/in-app", {
        method: "PUT",
        body: JSON.stringify({ enabled }),
      }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: notificationSettingsKey }),
  });
}

export function useConnectWebhook() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ channel, webhookUrl }: { channel: string; webhookUrl: string }) =>
      apiRequest("/api/v1/notification-channels", {
        method: "POST",
        body: JSON.stringify({ channel, webhookUrl }),
      }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: notificationSettingsKey }),
  });
}

export function useDisconnectChannel() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (channel: string) =>
      apiRequest(`/api/v1/notification-channels/${channel}`, { method: "DELETE" }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: notificationSettingsKey }),
  });
}

export function useSetPairAlert() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ pairId, enabled }: { pairId: string; enabled: boolean }) =>
      apiRequest("/api/v1/notification-alerts", {
        method: "PUT",
        body: JSON.stringify({ entityPairId: pairId, enabled }),
      }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: notificationSettingsKey }),
  });
}
