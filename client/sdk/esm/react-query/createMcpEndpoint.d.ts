import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { McpEndpoint } from "../models/components/mcpendpoint.js";
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
  CreateMcpEndpointRequest,
  CreateMcpEndpointSecurity,
} from "../models/operations/createmcpendpoint.js";
import { MutationHookOptions } from "./_types.js";
export type CreateMcpEndpointMutationVariables = {
  request: CreateMcpEndpointRequest;
  security?: CreateMcpEndpointSecurity | undefined;
  options?: RequestOptions;
};
export type CreateMcpEndpointMutationData = McpEndpoint;
export type CreateMcpEndpointMutationError =
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
 * createMcpEndpoint mcpEndpoints
 *
 * @remarks
 * Create a new MCP endpoint for an MCP server
 */
export declare function useCreateMcpEndpointMutation(
  options?: MutationHookOptions<
    CreateMcpEndpointMutationData,
    CreateMcpEndpointMutationError,
    CreateMcpEndpointMutationVariables
  >,
): UseMutationResult<
  CreateMcpEndpointMutationData,
  CreateMcpEndpointMutationError,
  CreateMcpEndpointMutationVariables
>;
export declare function mutationKeyCreateMcpEndpoint(): MutationKey;
export declare function buildCreateMcpEndpointMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateMcpEndpointMutationVariables,
  ) => Promise<CreateMcpEndpointMutationData>;
};
//# sourceMappingURL=createMcpEndpoint.d.ts.map
