import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Toolset } from "../models/components/toolset.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { AddOAuthProxyServerRequest, AddOAuthProxyServerSecurity } from "../models/operations/addoauthproxyserver.js";
import { MutationHookOptions } from "./_types.js";
export type AddOAuthProxyServerMutationVariables = {
    request: AddOAuthProxyServerRequest;
    security?: AddOAuthProxyServerSecurity | undefined;
    options?: RequestOptions;
};
export type AddOAuthProxyServerMutationData = Toolset;
export type AddOAuthProxyServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * addOAuthProxyServer toolsets
 *
 * @remarks
 * Associate an OAuth proxy server with a toolset (admin only)
 */
export declare function useAddOAuthProxyServerMutation(options?: MutationHookOptions<AddOAuthProxyServerMutationData, AddOAuthProxyServerMutationError, AddOAuthProxyServerMutationVariables>): UseMutationResult<AddOAuthProxyServerMutationData, AddOAuthProxyServerMutationError, AddOAuthProxyServerMutationVariables>;
export declare function mutationKeyAddOAuthProxyServer(): MutationKey;
export declare function buildAddOAuthProxyServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: AddOAuthProxyServerMutationVariables) => Promise<AddOAuthProxyServerMutationData>;
};
//# sourceMappingURL=addOAuthProxyServer.d.ts.map