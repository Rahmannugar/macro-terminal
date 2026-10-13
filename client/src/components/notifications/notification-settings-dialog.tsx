import { useState } from "react";
import { adminErrorMessage } from "../../lib/admin";
import {
  useConnectWebhook,
  useDisconnectChannel,
  useNotificationSettings,
  useSetInAppChannel,
} from "../../lib/notifications";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Input } from "../ui/input";
import { Skeleton } from "../ui/skeleton";

const webhookChannels = [
  { channel: "slack", label: "Slack" },
  { channel: "discord", label: "Discord" },
] as const;

export function NotificationSettingsDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const settings = useNotificationSettings();
  const setInApp = useSetInAppChannel();
  const connect = useConnectWebhook();
  const disconnect = useDisconnectChannel();
  const [slackURL, setSlackURL] = useState("");
  const [discordURL, setDiscordURL] = useState("");

  const error =
    adminErrorMessage(settings.error) ??
    adminErrorMessage(setInApp.error) ??
    adminErrorMessage(connect.error) ??
    adminErrorMessage(disconnect.error);

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Notification settings"
      description="Choose where Macro Terminal delivers your alert notifications."
    >
      {settings.isPending ? (
        <div className="mt-4 flex flex-col gap-3">
          <Skeleton className="h-10 rounded-lg" />
          <Skeleton className="h-10 rounded-lg" />
          <Skeleton className="h-10 rounded-lg" />
        </div>
      ) : (
        <div className="mt-4 flex flex-col gap-4">
          <div className="flex items-center justify-between gap-3 rounded-lg border border-border px-3.5 py-3">
            <div className="min-w-0">
              <p className="text-sm font-medium">In-app notifications</p>
              <p className="mt-0.5 text-xs text-muted-foreground">
                Bell and notifications page inside Macro Terminal.
              </p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={settings.data?.inApp ?? false}
              aria-label="In-app notifications"
              disabled={setInApp.isPending}
              className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${
                settings.data?.inApp ? "bg-primary" : "bg-secondary"
              }`}
              onClick={() => setInApp.mutate(!(settings.data?.inApp ?? false))}
            >
              <span
                className={`absolute top-0.5 block size-5 rounded-full bg-background shadow transition-transform ${
                  settings.data?.inApp ? "translate-x-5.5" : "translate-x-0.5"
                }`}
              />
            </button>
          </div>

          {webhookChannels.map(({ channel, label }) => {
            const connected = settings.data?.channels.find((row) => row.channel === channel);
            return (
              <div
                key={channel}
                className="flex flex-col gap-2.5 rounded-lg border border-border px-3.5 py-3"
              >
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <p className="text-sm font-medium">{label}</p>
                    <p className="mt-0.5 truncate text-xs text-muted-foreground">
                      {connected
                        ? (connected.endpoint ?? "Connected")
                        : `Paste the channel webhook URL to deliver alerts to ${label}.`}
                    </p>
                  </div>
                  {connected ? (
                    <Button
                      variant="secondary"
                      className="h-8! shrink-0 px-3! text-xs"
                      disabled={disconnect.isPending}
                      onClick={() => disconnect.mutate(channel)}
                    >
                      Disconnect
                    </Button>
                  ) : null}
                </div>
                {connected ? null : (
                  <form
                    className="flex gap-2"
                    onSubmit={(event) => {
                      event.preventDefault();
                      const value = channel === "slack" ? slackURL : discordURL;
                      if (!value.trim()) return;
                      connect.mutate(
                        { channel, webhookUrl: value.trim() },
                        {
                          onSuccess: () => {
                            if (channel === "slack") setSlackURL("");
                            else setDiscordURL("");
                          },
                        },
                      );
                    }}
                  >
                    <Input
                      type="url"
                      required
                      value={channel === "slack" ? slackURL : discordURL}
                      onChange={(event) =>
                        channel === "slack"
                          ? setSlackURL(event.target.value)
                          : setDiscordURL(event.target.value)
                      }
                      placeholder={
                        channel === "slack"
                          ? "https://hooks.slack.com/services/…"
                          : "https://discord.com/api/webhooks/…"
                      }
                      aria-label={`${label} webhook URL`}
                    />
                    <Button
                      type="submit"
                      variant="secondary"
                      className="h-9! shrink-0 px-3! text-xs"
                      pending={connect.isPending}
                    >
                      Connect
                    </Button>
                  </form>
                )}
              </div>
            );
          })}

          {error ? <p className="text-sm text-red-600">{error}</p> : null}
        </div>
      )}
    </Dialog>
  );
}
