import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteMcpServer } from "../models/components/remotemcpserver.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateRemoteMcpServerRequest, UpdateRemoteMcpServerSecurity } from "../models/operations/updateremotemcpserver.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateRemoteMcpServerMutationVariables = {
    request: UpdateRemoteMcpServerRequest;
    security?: UpdateRemoteMcpServerSecurity | undefined;
    options?: RequestOptions;
};
export type UpdateRemoteMcpServerMutationData = RemoteMcpServer;
export type UpdateRemoteMcpServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateServer remoteMcp
 *
 * @remarks
 * Update a remote MCP server
 */
export declare function useUpdateRemoteMcpServerMutation(options?: MutationHookOptions<UpdateRemoteMcpServerMutationData, UpdateRemoteMcpServerMutationError, UpdateRemoteMcpServerMutationVariables>): UseMutationResult<UpdateRemoteMcpServerMutationData, UpdateRemoteMcpServerMutationError, UpdateRemoteMcpServerMutationVariables>;
export declare function mutationKeyUpdateRemoteMcpServer(): MutationKey;
export declare function buildUpdateRemoteMcpServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpdateRemoteMcpServerMutationVariables) => Promise<UpdateRemoteMcpServerMutationData>;
};
//# sourceMappingURL=updateRemoteMcpServer.d.ts.map