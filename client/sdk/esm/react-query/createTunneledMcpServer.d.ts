import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreateTunneledMcpServerResult } from "../models/components/createtunneledmcpserverresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateTunneledMcpServerRequest, CreateTunneledMcpServerSecurity } from "../models/operations/createtunneledmcpserver.js";
import { MutationHookOptions } from "./_types.js";
export type CreateTunneledMcpServerMutationVariables = {
    request: CreateTunneledMcpServerRequest;
    security?: CreateTunneledMcpServerSecurity | undefined;
    options?: RequestOptions;
};
export type CreateTunneledMcpServerMutationData = CreateTunneledMcpServerResult;
export type CreateTunneledMcpServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createServer tunneledMcp
 *
 * @remarks
 * Create a new tunneled MCP server source. Returns the tunnel key once.
 */
export declare function useCreateTunneledMcpServerMutation(options?: MutationHookOptions<CreateTunneledMcpServerMutationData, CreateTunneledMcpServerMutationError, CreateTunneledMcpServerMutationVariables>): UseMutationResult<CreateTunneledMcpServerMutationData, CreateTunneledMcpServerMutationError, CreateTunneledMcpServerMutationVariables>;
export declare function mutationKeyCreateTunneledMcpServer(): MutationKey;
export declare function buildCreateTunneledMcpServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateTunneledMcpServerMutationVariables) => Promise<CreateTunneledMcpServerMutationData>;
};
//# sourceMappingURL=createTunneledMcpServer.d.ts.map