import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ExternalMCPServer } from "../models/components/externalmcpserver.js";
import {
  GetMCPServerDetailsRequest,
  GetMCPServerDetailsSecurity,
} from "../models/operations/getmcpserverdetails.js";
export type McpRegistriesGetServerDetailsQueryData = ExternalMCPServer;
export declare function prefetchMcpRegistriesGetServerDetails(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetMCPServerDetailsRequest,
  security?: GetMCPServerDetailsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildMcpRegistriesGetServerDetailsQuery(
  client$: GramCore,
  request: GetMCPServerDetailsRequest,
  security?: GetMCPServerDetailsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<McpRegistriesGetServerDetailsQueryData>;
};
export declare function queryKeyMcpRegistriesGetServerDetails(parameters: {
  registryId: string;
  serverSpecifier: string;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=mcpRegistriesGetServerDetails.core.d.ts.map
