import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteRemoteMcpServerRequest, DeleteRemoteMcpServerSecurity } from "../models/operations/deleteremotemcpserver.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteRemoteMcpServerMutationVariables = {
    request: DeleteRemoteMcpServerRequest;
    security?: DeleteRemoteMcpServerSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteRemoteMcpServerMutationData = void;
export type DeleteRemoteMcpServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteServer remoteMcp
 *
 * @remarks
 * Delete a remote MCP server
 */
export declare function useDeleteRemoteMcpServerMutation(options?: MutationHookOptions<DeleteRemoteMcpServerMutationData, DeleteRemoteMcpServerMutationError, DeleteRemoteMcpServerMutationVariables>): UseMutationResult<DeleteRemoteMcpServerMutationData, DeleteRemoteMcpServerMutationError, DeleteRemoteMcpServerMutationVariables>;
export declare function mutationKeyDeleteRemoteMcpServer(): MutationKey;
export declare function buildDeleteRemoteMcpServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteRemoteMcpServerMutationVariables) => Promise<DeleteRemoteMcpServerMutationData>;
};
//# sourceMappingURL=deleteRemoteMcpServer.d.ts.map