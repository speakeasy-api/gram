import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type NotificationsUnreadCountQueryData = components.UnreadCountResult;
export declare function prefetchNotificationsUnreadCount(
  queryClient: QueryClient,
  client$: GramCore,
  request?: operations.GetUnreadCountRequest | undefined,
  security?: operations.GetUnreadCountSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildNotificationsUnreadCountQuery(
  client$: GramCore,
  request?: operations.GetUnreadCountRequest | undefined,
  security?: operations.GetUnreadCountSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<NotificationsUnreadCountQueryData>;
};
export declare function queryKeyNotificationsUnreadCount(parameters: {
  since?: Date | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=notificationsUnreadCount.core.d.ts.map
