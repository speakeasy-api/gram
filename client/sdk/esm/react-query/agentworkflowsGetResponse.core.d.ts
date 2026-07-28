import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type AgentworkflowsGetResponseQueryData =
  components.WorkflowAgentResponseOutput;
export declare function prefetchAgentworkflowsGetResponse(
  queryClient: QueryClient,
  client$: GramCore,
  request: operations.GetResponseRequest,
  security?: operations.GetResponseSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildAgentworkflowsGetResponseQuery(
  client$: GramCore,
  request: operations.GetResponseRequest,
  security?: operations.GetResponseSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<AgentworkflowsGetResponseQueryData>;
};
export declare function queryKeyAgentworkflowsGetResponse(parameters: {
  responseId: string;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=agentworkflowsGetResponse.core.d.ts.map
