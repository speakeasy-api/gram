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
  CreateMcpServerRequest,
  CreateMcpServerSecurity,
} from "../models/operations/createmcpserver.js";
import { MutationHookOptions } from "./_types.js";
export type CreateMcpServerMutationVariables = {
  request: CreateMcpServerRequest;
  security?: CreateMcpServerSecurity | undefined;
  options?: RequestOptions;
};
export type CreateMcpServerMutationData = McpServer;
export type CreateMcpServerMutationError =
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
 * createMcpServer mcpServers
 *
 * @remarks
 * Create a new MCP server
 */
export declare function useCreateMcpServerMutation(
  options?: MutationHookOptions<
    CreateMcpServerMutationData,
    CreateMcpServerMutationError,
    CreateMcpServerMutationVariables
  >,
): UseMutationResult<
  CreateMcpServerMutationData,
  CreateMcpServerMutationError,
  CreateMcpServerMutationVariables
>;
export declare function mutationKeyCreateMcpServer(): MutationKey;
export declare function buildCreateMcpServerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateMcpServerMutationVariables,
  ) => Promise<CreateMcpServerMutationData>;
};
//# sourceMappingURL=createMcpServer.d.ts.map
