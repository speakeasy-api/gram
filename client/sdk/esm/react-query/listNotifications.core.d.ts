import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type ListNotificationsQueryData = components.ListNotificationsResult;
export declare function prefetchListNotifications(
  queryClient: QueryClient,
  client$: GramCore,
  request?: operations.ListNotificationsRequest | undefined,
  security?: operations.ListNotificationsSecurity | undefined,
  options?: RequestOptions,
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
//# sourceMappingURL=listNotifications.core.d.ts.map
