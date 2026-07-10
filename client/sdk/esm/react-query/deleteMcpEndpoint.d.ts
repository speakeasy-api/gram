import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  DeleteMcpEndpointRequest,
  DeleteMcpEndpointSecurity,
} from "../models/operations/deletemcpendpoint.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteMcpEndpointMutationVariables = {
  request: DeleteMcpEndpointRequest;
  security?: DeleteMcpEndpointSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteMcpEndpointMutationData = void;
export type DeleteMcpEndpointMutationError =
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
 * deleteMcpEndpoint mcpEndpoints
 *
 * @remarks
 * Delete an MCP endpoint
 */
export declare function useDeleteMcpEndpointMutation(
  options?: MutationHookOptions<
    DeleteMcpEndpointMutationData,
    DeleteMcpEndpointMutationError,
    DeleteMcpEndpointMutationVariables
  >,
): UseMutationResult<
  DeleteMcpEndpointMutationData,
  DeleteMcpEndpointMutationError,
  DeleteMcpEndpointMutationVariables
>;
export declare function mutationKeyDeleteMcpEndpoint(): MutationKey;
export declare function buildDeleteMcpEndpointMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteMcpEndpointMutationVariables,
  ) => Promise<DeleteMcpEndpointMutationData>;
};
//# sourceMappingURL=deleteMcpEndpoint.d.ts.map
