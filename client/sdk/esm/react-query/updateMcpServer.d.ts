import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { McpServer } from "../models/components/mcpserver.js";
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
  UpdateMcpServerRequest,
  UpdateMcpServerSecurity,
} from "../models/operations/updatemcpserver.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateMcpServerMutationVariables = {
  request: UpdateMcpServerRequest;
  security?: UpdateMcpServerSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateMcpServerMutationData = McpServer;
export type UpdateMcpServerMutationError =
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
 * updateMcpServer mcpServers
 *
 * @remarks
 * Update an MCP server. This is a full-record replace for the optional UUID references: fields omitted from the request become null on the stored record. name is an exception — omitting it leaves the existing display name unchanged, while providing it requires a non-empty value and recomputes the server-side slug. The id and visibility fields are required; exactly one of remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id must be provided.
 */
export declare function useUpdateMcpServerMutation(
  options?: MutationHookOptions<
    UpdateMcpServerMutationData,
    UpdateMcpServerMutationError,
    UpdateMcpServerMutationVariables
  >,
): UseMutationResult<
  UpdateMcpServerMutationData,
  UpdateMcpServerMutationError,
  UpdateMcpServerMutationVariables
>;
export declare function mutationKeyUpdateMcpServer(): MutationKey;
export declare function buildUpdateMcpServerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateMcpServerMutationVariables,
  ) => Promise<UpdateMcpServerMutationData>;
};
//# sourceMappingURL=updateMcpServer.d.ts.map
