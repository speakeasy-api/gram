import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListTriggerInstancesResult } from "../models/components/listtriggerinstancesresult.js";
import {
  ListTriggerInstancesRequest,
  ListTriggerInstancesSecurity,
} from "../models/operations/listtriggerinstances.js";
export type TriggersQueryData = ListTriggerInstancesResult;
export declare function prefetchTriggers(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListTriggerInstancesRequest | undefined,
  security?: ListTriggerInstancesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildTriggersQuery(
  client$: GramCore,
  request?: ListTriggerInstancesRequest | undefined,
  security?: ListTriggerInstancesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<TriggersQueryData>;
};
export declare function queryKeyTriggers(parameters: {
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=triggers.core.d.ts.map
