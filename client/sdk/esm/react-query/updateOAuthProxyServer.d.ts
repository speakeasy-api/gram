import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Toolset } from "../models/components/toolset.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateOAuthProxyServerRequest, UpdateOAuthProxyServerSecurity } from "../models/operations/updateoauthproxyserver.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateOAuthProxyServerMutationVariables = {
    request: UpdateOAuthProxyServerRequest;
    security?: UpdateOAuthProxyServerSecurity | undefined;
    options?: RequestOptions;
};
export type UpdateOAuthProxyServerMutationData = Toolset;
export type UpdateOAuthProxyServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateOAuthProxyServer toolsets
 *
 * @remarks
 * Update an existing OAuth proxy server associated with a toolset
 */
export declare function useUpdateOAuthProxyServerMutation(options?: MutationHookOptions<UpdateOAuthProxyServerMutationData, UpdateOAuthProxyServerMutationError, UpdateOAuthProxyServerMutationVariables>): UseMutationResult<UpdateOAuthProxyServerMutationData, UpdateOAuthProxyServerMutationError, UpdateOAuthProxyServerMutationVariables>;
export declare function mutationKeyUpdateOAuthProxyServer(): MutationKey;
export declare function buildUpdateOAuthProxyServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpdateOAuthProxyServerMutationVariables) => Promise<UpdateOAuthProxyServerMutationData>;
};
//# sourceMappingURL=updateOAuthProxyServer.d.ts.map