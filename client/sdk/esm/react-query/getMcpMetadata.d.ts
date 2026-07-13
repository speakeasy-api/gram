import {
  InvalidateQueryFilters,
  QueryClient,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  GetMcpMetadataRequest,
  GetMcpMetadataSecurity,
} from "../models/operations/getmcpmetadata.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetMcpMetadataQuery,
  GetMcpMetadataQueryData,
  prefetchGetMcpMetadata,
  queryKeyGetMcpMetadata,
} from "./getMcpMetadata.core.js";
export {
  buildGetMcpMetadataQuery,
  type GetMcpMetadataQueryData,
  prefetchGetMcpMetadata,
  queryKeyGetMcpMetadata,
};
export type GetMcpMetadataQueryError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * getMcpMetadata mcpMetadata
 *
 * @remarks
 * Fetch the metadata that powers the MCP install page. Exactly one of toolset_slug or mcp_server_id must be provided.
 */
export declare function useGetMcpMetadata(
  request?: GetMcpMetadataRequest | undefined,
  security?: GetMcpMetadataSecurity | undefined,
  options?: QueryHookOptions<GetMcpMetadataQueryData, GetMcpMetadataQueryError>,
): UseQueryResult<GetMcpMetadataQueryData, GetMcpMetadataQueryError>;
/**
 * getMcpMetadata mcpMetadata
 *
 * @remarks
 * Fetch the metadata that powers the MCP install page. Exactly one of toolset_slug or mcp_server_id must be provided.
 */
export declare function useGetMcpMetadataSuspense(
  request?: GetMcpMetadataRequest | undefined,
  security?: GetMcpMetadataSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GetMcpMetadataQueryData,
    GetMcpMetadataQueryError
  >,
): UseSuspenseQueryResult<GetMcpMetadataQueryData, GetMcpMetadataQueryError>;
export declare function setGetMcpMetadataData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      toolsetSlug?: string | undefined;
      mcpServerId?: string | undefined;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: GetMcpMetadataQueryData,
): GetMcpMetadataQueryData | undefined;
export declare function invalidateGetMcpMetadata(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        toolsetSlug?: string | undefined;
        mcpServerId?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGetMcpMetadata(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getMcpMetadata.d.ts.map
