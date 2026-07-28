import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AssistantMemory } from "../models/components/assistantmemory.js";
import {
  GetAssistantMemoryRequest,
  GetAssistantMemorySecurity,
} from "../models/operations/getassistantmemory.js";
export type GetAssistantMemoryQueryData = AssistantMemory;
export declare function prefetchGetAssistantMemory(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetAssistantMemoryRequest,
  security?: GetAssistantMemorySecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetAssistantMemoryQuery(
  client$: GramCore,
  request: GetAssistantMemoryRequest,
  security?: GetAssistantMemorySecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GetAssistantMemoryQueryData>;
};
export declare function queryKeyGetAssistantMemory(parameters: {
  id: string;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getAssistantMemory.core.d.ts.map
