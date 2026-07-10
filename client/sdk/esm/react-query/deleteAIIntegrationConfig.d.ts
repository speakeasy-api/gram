import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteAIIntegrationConfigRequest, DeleteAIIntegrationConfigSecurity } from "../models/operations/deleteaiintegrationconfig.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteAIIntegrationConfigMutationVariables = {
    request: DeleteAIIntegrationConfigRequest;
    security?: DeleteAIIntegrationConfigSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteAIIntegrationConfigMutationData = void;
export type DeleteAIIntegrationConfigMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteConfig aiIntegrations
 *
 * @remarks
 * Delete the org-wide AI integration config for a provider.
 */
export declare function useDeleteAIIntegrationConfigMutation(options?: MutationHookOptions<DeleteAIIntegrationConfigMutationData, DeleteAIIntegrationConfigMutationError, DeleteAIIntegrationConfigMutationVariables>): UseMutationResult<DeleteAIIntegrationConfigMutationData, DeleteAIIntegrationConfigMutationError, DeleteAIIntegrationConfigMutationVariables>;
export declare function mutationKeyDeleteAIIntegrationConfig(): MutationKey;
export declare function buildDeleteAIIntegrationConfigMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteAIIntegrationConfigMutationVariables) => Promise<DeleteAIIntegrationConfigMutationData>;
};
//# sourceMappingURL=deleteAIIntegrationConfig.d.ts.map