import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { SetOrganizationWhitelistRequest, SetOrganizationWhitelistSecurity } from "../models/operations/setorganizationwhitelist.js";
import { MutationHookOptions } from "./_types.js";
export type ProjectsSetOrganizationWhitelistMutationVariables = {
    request: SetOrganizationWhitelistRequest;
    security?: SetOrganizationWhitelistSecurity | undefined;
    options?: RequestOptions;
};
export type ProjectsSetOrganizationWhitelistMutationData = void;
export type ProjectsSetOrganizationWhitelistMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * setOrganizationWhitelist projects
 *
 * @remarks
 * Set organization whitelist status (admin only - requires speakeasy-team API key)
 */
export declare function useProjectsSetOrganizationWhitelistMutation(options?: MutationHookOptions<ProjectsSetOrganizationWhitelistMutationData, ProjectsSetOrganizationWhitelistMutationError, ProjectsSetOrganizationWhitelistMutationVariables>): UseMutationResult<ProjectsSetOrganizationWhitelistMutationData, ProjectsSetOrganizationWhitelistMutationError, ProjectsSetOrganizationWhitelistMutationVariables>;
export declare function mutationKeyProjectsSetOrganizationWhitelist(): MutationKey;
export declare function buildProjectsSetOrganizationWhitelistMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: ProjectsSetOrganizationWhitelistMutationVariables) => Promise<ProjectsSetOrganizationWhitelistMutationData>;
};
//# sourceMappingURL=projectsSetOrganizationWhitelist.d.ts.map