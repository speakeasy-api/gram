import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type AgentsGetQueryData = components.AgentResponseOutput;
export declare function prefetchAgentsGet(
  queryClient: QueryClient,
  client$: GramCore,
  request: operations.GetAgentResponseRequest,
  security?: operations.GetAgentResponseSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildAgentsGetQuery(
  client$: GramCore,
  request: operations.GetAgentResponseRequest,
  security?: operations.GetAgentResponseSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<AgentsGetQueryData>;
};
export declare function queryKeyAgentsGet(parameters: {
  responseId: string;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=agentsGet.core.d.ts.map
