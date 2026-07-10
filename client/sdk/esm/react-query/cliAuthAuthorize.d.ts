import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AuthorizeResponseBody } from "../models/components/authorizeresponsebody.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CliAuthAuthorizeRequest, CliAuthAuthorizeSecurity } from "../models/operations/cliauthauthorize.js";
import { MutationHookOptions } from "./_types.js";
export type CliAuthAuthorizeMutationVariables = {
    request: CliAuthAuthorizeRequest;
    security?: CliAuthAuthorizeSecurity | undefined;
    options?: RequestOptions;
};
export type CliAuthAuthorizeMutationData = AuthorizeResponseBody;
export type CliAuthAuthorizeMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * authorize cliAuth
 *
 * @remarks
 * Mint a short-lived one-time code bound to a PKCE code_challenge, on behalf of the authenticated dashboard user. Resolves the target project (given slug, else the org's default/first project) and records {user, org, project, scopes:[agent,hooks], challenge} against the code with a ~5 minute TTL. Requires a member-available session (org:read); NOT org-admin.
 */
export declare function useCliAuthAuthorizeMutation(options?: MutationHookOptions<CliAuthAuthorizeMutationData, CliAuthAuthorizeMutationError, CliAuthAuthorizeMutationVariables>): UseMutationResult<CliAuthAuthorizeMutationData, CliAuthAuthorizeMutationError, CliAuthAuthorizeMutationVariables>;
export declare function mutationKeyCliAuthAuthorize(): MutationKey;
export declare function buildCliAuthAuthorizeMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CliAuthAuthorizeMutationVariables) => Promise<CliAuthAuthorizeMutationData>;
};
//# sourceMappingURL=cliAuthAuthorize.d.ts.map