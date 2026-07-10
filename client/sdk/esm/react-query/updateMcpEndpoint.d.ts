import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { McpEndpoint } from "../models/components/mcpendpoint.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateMcpEndpointRequest, UpdateMcpEndpointSecurity } from "../models/operations/updatemcpendpoint.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateMcpEndpointMutationVariables = {
    request: UpdateMcpEndpointRequest;
    security?: UpdateMcpEndpointSecurity | undefined;
    options?: RequestOptions;
};
export type UpdateMcpEndpointMutationData = McpEndpoint;
export type UpdateMcpEndpointMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateMcpEndpoint mcpEndpoints
 *
 * @remarks
 * Update an MCP endpoint. This is a full-record replace: fields omitted from the request become null on the stored record. The id, mcp_server_id, and slug fields are required.
 */
export declare function useUpdateMcpEndpointMutation(options?: MutationHookOptions<UpdateMcpEndpointMutationData, UpdateMcpEndpointMutationError, UpdateMcpEndpointMutationVariables>): UseMutationResult<UpdateMcpEndpointMutationData, UpdateMcpEndpointMutationError, UpdateMcpEndpointMutationVariables>;
export declare function mutationKeyUpdateMcpEndpoint(): MutationKey;
export declare function buildUpdateMcpEndpointMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpdateMcpEndpointMutationVariables) => Promise<UpdateMcpEndpointMutationData>;
};
//# sourceMappingURL=updateMcpEndpoint.d.ts.map