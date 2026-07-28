import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TunneledMcpServer } from "../models/components/tunneledmcpserver.js";
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
  UpdateTunneledMcpServerRequest,
  UpdateTunneledMcpServerSecurity,
} from "../models/operations/updatetunneledmcpserver.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateTunneledMcpServerMutationVariables = {
  request: UpdateTunneledMcpServerRequest;
  security?: UpdateTunneledMcpServerSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateTunneledMcpServerMutationData = TunneledMcpServer;
export type UpdateTunneledMcpServerMutationError =
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
 * updateServer tunneledMcp
 *
 * @remarks
 * Update a tunneled MCP server source
 */
export declare function useUpdateTunneledMcpServerMutation(
  options?: MutationHookOptions<
    UpdateTunneledMcpServerMutationData,
    UpdateTunneledMcpServerMutationError,
    UpdateTunneledMcpServerMutationVariables
  >,
): UseMutationResult<
  UpdateTunneledMcpServerMutationData,
  UpdateTunneledMcpServerMutationError,
  UpdateTunneledMcpServerMutationVariables
>;
export declare function mutationKeyUpdateTunneledMcpServer(): MutationKey;
export declare function buildUpdateTunneledMcpServerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateTunneledMcpServerMutationVariables,
  ) => Promise<UpdateTunneledMcpServerMutationData>;
};
//# sourceMappingURL=updateTunneledMcpServer.d.ts.map
