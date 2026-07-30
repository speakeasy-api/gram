import {
  InvalidateQueryFilters,
  QueryClient,
  QueryFunctionContext,
  QueryKey,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
export type NotificationsUnreadCountQueryData = components.UnreadCountResult;
/**
 * getUnreadCount notifications
 *
 * @remarks
 * Get the count of notifications created since a given timestamp
 */
export declare function useNotificationsUnreadCount(
  request?: operations.GetUnreadCountRequest | undefined,
  security?: operations.GetUnreadCountSecurity | undefined,
  options?: QueryHookOptions<NotificationsUnreadCountQueryData>,
): UseQueryResult<NotificationsUnreadCountQueryData, Error>;
/**
 * getUnreadCount notifications
 *
 * @remarks
 * Get the count of notifications created since a given timestamp
 */
export declare function useNotificationsUnreadCountSuspense(
  request?: operations.GetUnreadCountRequest | undefined,
  security?: operations.GetUnreadCountSecurity | undefined,
  options?: SuspenseQueryHookOptions<NotificationsUnreadCountQueryData>,
): UseSuspenseQueryResult<NotificationsUnreadCountQueryData, Error>;
export declare function prefetchNotificationsUnreadCount(
  queryClient: QueryClient,
  client$: GramCore,
  request?: operations.GetUnreadCountRequest | undefined,
  security?: operations.GetUnreadCountSecurity | undefined,
): Promise<void>;
export declare function setNotificationsUnreadCountData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      since?: Date | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: NotificationsUnreadCountQueryData,
): NotificationsUnreadCountQueryData | undefined;
export declare function invalidateNotificationsUnreadCount(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        since?: Date | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllNotificationsUnreadCount(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
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
//# sourceMappingURL=notificationsUnreadCount.d.ts.map
