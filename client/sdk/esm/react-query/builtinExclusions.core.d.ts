import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListBuiltinExclusionsResult } from "../models/components/listbuiltinexclusionsresult.js";
import {
  ListBuiltinExclusionsRequest,
  ListBuiltinExclusionsSecurity,
} from "../models/operations/listbuiltinexclusions.js";
export type BuiltinExclusionsQueryData = ListBuiltinExclusionsResult;
export declare function prefetchBuiltinExclusions(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListBuiltinExclusionsRequest | undefined,
  security?: ListBuiltinExclusionsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildBuiltinExclusionsQuery(
  client$: GramCore,
  request?: ListBuiltinExclusionsRequest | undefined,
  security?: ListBuiltinExclusionsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<BuiltinExclusionsQueryData>;
};
export declare function queryKeyBuiltinExclusions(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=builtinExclusions.core.d.ts.map
