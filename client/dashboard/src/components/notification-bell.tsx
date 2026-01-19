import { useState, useCallback } from "react";
import { Bell, Check, AlertCircle, Info, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";
import {
  useListNotifications,
  useNotificationsUnreadCount,
  useArchiveNotificationMutation,
  queryKeyListNotifications,
  queryKeyNotificationsUnreadCount,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { formatDistanceToNow } from "date-fns";

const LAST_VIEWED_KEY = "notifications-last-viewed";

function getLastViewedAt(): string | null {
  if (typeof localStorage === "undefined") return null;
  return localStorage.getItem(LAST_VIEWED_KEY);
}

function setLastViewedAt(timestamp: string) {
  if (typeof localStorage === "undefined") return;
  localStorage.setItem(LAST_VIEWED_KEY, timestamp);
}

export function NotificationBell() {
  const [open, setOpen] = useState(false);
  const [activeTab, setActiveTab] = useState<"active" | "archived">("active");
  const [lastViewedAt, setLastViewedAtState] = useState<string | null>(
    getLastViewedAt,
  );

  const queryClient = useQueryClient();

  const { data: unreadData } = useNotificationsUnreadCount(
    lastViewedAt ? { since: new Date(lastViewedAt) } : undefined,
    undefined,
    {
      refetchInterval: 30000, // Refresh every 30 seconds
    },
  );

  const { data: notificationsData, isLoading } = useListNotifications(
    {
      archived: activeTab === "archived",
      limit: 20,
    },
    undefined,
    {
      enabled: open,
    },
  );

  const archiveMutation = useArchiveNotificationMutation({
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: queryKeyListNotifications({ archived: false, limit: 20 }),
      });
      queryClient.invalidateQueries({
        queryKey: queryKeyListNotifications({ archived: true, limit: 20 }),
      });
      queryClient.invalidateQueries({
        queryKey: queryKeyNotificationsUnreadCount(
          lastViewedAt ? { since: new Date(lastViewedAt) } : {},
        ),
      });
    },
  });

  const handleOpenChange = useCallback((isOpen: boolean) => {
    setOpen(isOpen);
    if (isOpen) {
      const now = new Date().toISOString();
      setLastViewedAt(now);
      setLastViewedAtState(now);
    }
  }, []);

  const handleArchive = useCallback(
    (id: string) => {
      archiveMutation.mutate({
        request: { archiveNotificationRequestBody: { id } },
      });
    },
    [archiveMutation],
  );

  const unreadCount = unreadData?.count ?? 0;
  const notifications = notificationsData?.notifications ?? [];

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" className="relative">
          <Bell className="h-5 w-5" />
          {unreadCount > 0 && (
            <span className="absolute -top-1 -right-1 h-4 w-4 rounded-full bg-destructive text-[10px] text-white flex items-center justify-center font-medium">
              {unreadCount > 9 ? "9+" : unreadCount}
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-80 p-0" align="end" sideOffset={8}>
        <Tabs
          value={activeTab}
          onValueChange={(v) => setActiveTab(v as "active" | "archived")}
        >
          <div className="p-3 border-b">
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="active">Active</TabsTrigger>
              <TabsTrigger value="archived">Archived</TabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="active" className="m-0">
            <NotificationList
              notifications={notifications}
              isLoading={isLoading}
              emptyMessage="No active notifications"
              onArchive={handleArchive}
              showArchiveButton
            />
          </TabsContent>

          <TabsContent value="archived" className="m-0">
            <NotificationList
              notifications={notifications}
              isLoading={isLoading}
              emptyMessage="No archived notifications"
            />
          </TabsContent>
        </Tabs>
      </PopoverContent>
    </Popover>
  );
}

interface Notification {
  id: string;
  type: string;
  level: string;
  title: string;
  message?: string | null;
  createdAt: Date;
  archivedAt?: Date | null;
}

interface NotificationListProps {
  notifications: Notification[];
  isLoading: boolean;
  emptyMessage: string;
  onArchive?: (id: string) => void;
  showArchiveButton?: boolean;
}

function NotificationList({
  notifications,
  isLoading,
  emptyMessage,
  onArchive,
  showArchiveButton,
}: NotificationListProps) {
  if (isLoading) {
    return (
      <div className="p-6 text-center">
        <Type small muted>
          Loading...
        </Type>
      </div>
    );
  }

  if (notifications.length === 0) {
    return (
      <div className="p-6 text-center">
        <Type small muted>
          {emptyMessage}
        </Type>
      </div>
    );
  }

  return (
    <div className="max-h-80 overflow-y-auto">
      {notifications.map((notification) => (
        <NotificationItem
          key={notification.id}
          notification={notification}
          onArchive={onArchive}
          showArchiveButton={showArchiveButton}
        />
      ))}
    </div>
  );
}

interface NotificationItemProps {
  notification: Notification;
  onArchive?: (id: string) => void;
  showArchiveButton?: boolean;
}

function NotificationItem({
  notification,
  onArchive,
  showArchiveButton,
}: NotificationItemProps) {
  const LevelIcon =
    {
      info: Info,
      success: Check,
      warning: AlertTriangle,
      error: AlertCircle,
    }[notification.level] ?? Info;

  const levelColorClass =
    {
      info: "text-muted-foreground",
      success: "text-green-600",
      warning: "text-yellow-600",
      error: "text-destructive",
    }[notification.level] ?? "text-muted-foreground";

  return (
    <div className="p-3 border-b last:border-b-0 hover:bg-muted/50 transition-colors">
      <Stack direction="horizontal" gap={2} align="start">
        <LevelIcon className={`h-4 w-4 mt-0.5 shrink-0 ${levelColorClass}`} />
        <Stack direction="vertical" gap={1} className="flex-1 min-w-0">
          <Stack
            direction="horizontal"
            justify="space-between"
            align="start"
            gap={2}
          >
            <div className="min-w-0 flex-1">
              <Type small className="font-medium line-clamp-2">
                {notification.title}
              </Type>
            </div>
            {showArchiveButton && onArchive && (
              <Button
                variant="ghost"
                size="sm"
                className="h-6 px-2 text-xs shrink-0"
                onClick={() => onArchive(notification.id)}
              >
                Archive
              </Button>
            )}
          </Stack>
          {notification.message && (
            <Type small muted className="line-clamp-2">
              {notification.message}
            </Type>
          )}
          <Type small muted className="text-xs">
            {formatDistanceToNow(notification.createdAt, {
              addSuffix: true,
            })}
          </Type>
        </Stack>
      </Stack>
    </div>
  );
}

export default NotificationBell;
