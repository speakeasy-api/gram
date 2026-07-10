import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteMcpServerRequest, DeleteMcpServerSecurity } from "../models/operations/deletemcpserver.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteMcpServerMutationVariables = {
    request: DeleteMcpServerRequest;
    security?: DeleteMcpServerSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteMcpServerMutationData = void;
export type DeleteMcpServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteMcpServer mcpServers
 *
 * @remarks
 * Delete an MCP server
 */
export declare function useDeleteMcpServerMutation(options?: MutationHookOptions<DeleteMcpServerMutationData, DeleteMcpServerMutationError, DeleteMcpServerMutationVariables>): UseMutationResult<DeleteMcpServerMutationData, DeleteMcpServerMutationError, DeleteMcpServerMutationVariables>;
export declare function mutationKeyDeleteMcpServer(): MutationKey;
export declare function buildDeleteMcpServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteMcpServerMutationVariables) => Promise<DeleteMcpServerMutationData>;
};
//# sourceMappingURL=deleteMcpServer.d.ts.map