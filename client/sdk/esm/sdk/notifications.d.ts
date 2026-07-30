import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export declare class Notifications extends ClientSDK {
  /**
   * archiveNotification notifications
   *
   * @remarks
   * Archive a notification
   */
  archive(
    request: operations.ArchiveNotificationRequest,
    security?: operations.ArchiveNotificationSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.Notification>;
  /**
   * createNotification notifications
   *
   * @remarks
   * Create a notification for the current project
   */
  create(
    request: operations.CreateNotificationRequest,
    security?: operations.CreateNotificationSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.Notification>;
  /**
   * listNotifications notifications
   *
   * @remarks
   * List notifications for the current project
   */
  list(
    request?: operations.ListNotificationsRequest | undefined,
    security?: operations.ListNotificationsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.ListNotificationsResult>;
  /**
   * getUnreadCount notifications
   *
   * @remarks
   * Get the count of notifications created since a given timestamp
   */
  unreadCount(
    request?: operations.GetUnreadCountRequest | undefined,
    security?: operations.GetUnreadCountSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.UnreadCountResult>;
}
//# sourceMappingURL=notifications.d.ts.map
