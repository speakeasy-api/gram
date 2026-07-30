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
export type ListNotificationsQueryData = components.ListNotificationsResult;
/**
 * listNotifications notifications
 *
 * @remarks
 * List notifications for the current project
 */
export declare function useListNotifications(
  request?: operations.ListNotificationsRequest | undefined,
  security?: operations.ListNotificationsSecurity | undefined,
  options?: QueryHookOptions<ListNotificationsQueryData>,
): UseQueryResult<ListNotificationsQueryData, Error>;
/**
 * listNotifications notifications
 *
 * @remarks
 * List notifications for the current project
 */
export declare function useListNotificationsSuspense(
  request?: operations.ListNotificationsRequest | undefined,
  security?: operations.ListNotificationsSecurity | undefined,
  options?: SuspenseQueryHookOptions<ListNotificationsQueryData>,
): UseSuspenseQueryResult<ListNotificationsQueryData, Error>;
export declare function prefetchListNotifications(
  queryClient: QueryClient,
  client$: GramCore,
  request?: operations.ListNotificationsRequest | undefined,
  security?: operations.ListNotificationsSecurity | undefined,
): Promise<void>;
export declare function setListNotificationsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      archived?: boolean | undefined;
      limit?: number | undefined;
      cursor?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ListNotificationsQueryData,
): ListNotificationsQueryData | undefined;
export declare function invalidateListNotifications(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        archived?: boolean | undefined;
        limit?: number | undefined;
        cursor?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListNotifications(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function buildListNotificationsQuery(
  client$: GramCore,
  request?: operations.ListNotificationsRequest | undefined,
  security?: operations.ListNotificationsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ListNotificationsQueryData>;
};
export declare function queryKeyListNotifications(parameters: {
  archived?: boolean | undefined;
  limit?: number | undefined;
  cursor?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listNotifications.d.ts.map
