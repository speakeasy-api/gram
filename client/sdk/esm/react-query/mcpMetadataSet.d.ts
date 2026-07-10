import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { McpMetadata } from "../models/components/mcpmetadata.js";
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
  SetMcpMetadataRequest,
  SetMcpMetadataSecurity,
} from "../models/operations/setmcpmetadata.js";
import { MutationHookOptions } from "./_types.js";
export type McpMetadataSetMutationVariables = {
  request: SetMcpMetadataRequest;
  security?: SetMcpMetadataSecurity | undefined;
  options?: RequestOptions;
};
export type McpMetadataSetMutationData = McpMetadata;
export type McpMetadataSetMutationError =
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
 * setMcpMetadata mcpMetadata
 *
 * @remarks
 * Create or update the metadata that powers the MCP install page. Exactly one of toolset_slug or mcp_server_id must be provided.
 */
export declare function useMcpMetadataSetMutation(
  options?: MutationHookOptions<
    McpMetadataSetMutationData,
    McpMetadataSetMutationError,
    McpMetadataSetMutationVariables
  >,
): UseMutationResult<
  McpMetadataSetMutationData,
  McpMetadataSetMutationError,
  McpMetadataSetMutationVariables
>;
export declare function mutationKeyMcpMetadataSet(): MutationKey;
export declare function buildMcpMetadataSetMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: McpMetadataSetMutationVariables,
  ) => Promise<McpMetadataSetMutationData>;
};
//# sourceMappingURL=mcpMetadataSet.d.ts.map
