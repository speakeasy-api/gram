import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteMcpServer } from "../models/components/remotemcpserver.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateRemoteMcpServerRequest, CreateRemoteMcpServerSecurity } from "../models/operations/createremotemcpserver.js";
import { MutationHookOptions } from "./_types.js";
export type CreateRemoteMcpServerMutationVariables = {
    request: CreateRemoteMcpServerRequest;
    security?: CreateRemoteMcpServerSecurity | undefined;
    options?: RequestOptions;
};
export type CreateRemoteMcpServerMutationData = RemoteMcpServer;
export type CreateRemoteMcpServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createServer remoteMcp
 *
 * @remarks
 * Create a new remote MCP server
 */
export declare function useCreateRemoteMcpServerMutation(options?: MutationHookOptions<CreateRemoteMcpServerMutationData, CreateRemoteMcpServerMutationError, CreateRemoteMcpServerMutationVariables>): UseMutationResult<CreateRemoteMcpServerMutationData, CreateRemoteMcpServerMutationError, CreateRemoteMcpServerMutationVariables>;
export declare function mutationKeyCreateRemoteMcpServer(): MutationKey;
export declare function buildCreateRemoteMcpServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateRemoteMcpServerMutationVariables) => Promise<CreateRemoteMcpServerMutationData>;
};
//# sourceMappingURL=createRemoteMcpServer.d.ts.map