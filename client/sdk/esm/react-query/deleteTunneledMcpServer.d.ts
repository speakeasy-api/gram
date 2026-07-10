import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteTunneledMcpServerRequest, DeleteTunneledMcpServerSecurity } from "../models/operations/deletetunneledmcpserver.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteTunneledMcpServerMutationVariables = {
    request: DeleteTunneledMcpServerRequest;
    security?: DeleteTunneledMcpServerSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteTunneledMcpServerMutationData = void;
export type DeleteTunneledMcpServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteServer tunneledMcp
 *
 * @remarks
 * Delete a tunneled MCP server source
 */
export declare function useDeleteTunneledMcpServerMutation(options?: MutationHookOptions<DeleteTunneledMcpServerMutationData, DeleteTunneledMcpServerMutationError, DeleteTunneledMcpServerMutationVariables>): UseMutationResult<DeleteTunneledMcpServerMutationData, DeleteTunneledMcpServerMutationError, DeleteTunneledMcpServerMutationVariables>;
export declare function mutationKeyDeleteTunneledMcpServer(): MutationKey;
export declare function buildDeleteTunneledMcpServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteTunneledMcpServerMutationVariables) => Promise<DeleteTunneledMcpServerMutationData>;
};
//# sourceMappingURL=deleteTunneledMcpServer.d.ts.map