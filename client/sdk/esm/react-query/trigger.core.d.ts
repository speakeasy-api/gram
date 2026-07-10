import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TriggerInstance } from "../models/components/triggerinstance.js";
import {
  GetTriggerInstanceRequest,
  GetTriggerInstanceSecurity,
} from "../models/operations/gettriggerinstance.js";
export type TriggerQueryData = TriggerInstance;
export declare function prefetchTrigger(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetTriggerInstanceRequest,
  security?: GetTriggerInstanceSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildTriggerQuery(
  client$: GramCore,
  request: GetTriggerInstanceRequest,
  security?: GetTriggerInstanceSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<TriggerQueryData>;
};
export declare function queryKeyTrigger(parameters: {
  id: string;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=trigger.core.d.ts.map
