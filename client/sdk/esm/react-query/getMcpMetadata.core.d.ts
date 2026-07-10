import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetMcpMetadataResponseBody } from "../models/components/getmcpmetadataresponsebody.js";
import {
  GetMcpMetadataRequest,
  GetMcpMetadataSecurity,
} from "../models/operations/getmcpmetadata.js";
export type GetMcpMetadataQueryData = GetMcpMetadataResponseBody;
export declare function prefetchGetMcpMetadata(
  queryClient: QueryClient,
  client$: GramCore,
  request?: GetMcpMetadataRequest | undefined,
  security?: GetMcpMetadataSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetMcpMetadataQuery(
  client$: GramCore,
  request?: GetMcpMetadataRequest | undefined,
  security?: GetMcpMetadataSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<GetMcpMetadataQueryData>;
};
export declare function queryKeyGetMcpMetadata(parameters: {
  toolsetSlug?: string | undefined;
  mcpServerId?: string | undefined;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getMcpMetadata.core.d.ts.map
