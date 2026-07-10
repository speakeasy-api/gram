import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreateDeploymentResult } from "../models/components/createdeploymentresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateDeploymentRequest, CreateDeploymentSecurity } from "../models/operations/createdeployment.js";
import { MutationHookOptions } from "./_types.js";
export type CreateDeploymentMutationVariables = {
    request: CreateDeploymentRequest;
    security?: CreateDeploymentSecurity | undefined;
    options?: RequestOptions;
};
export type CreateDeploymentMutationData = CreateDeploymentResult;
export type CreateDeploymentMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createDeployment deployments
 *
 * @remarks
 * Create a deployment to load tool definitions.
 */
export declare function useCreateDeploymentMutation(options?: MutationHookOptions<CreateDeploymentMutationData, CreateDeploymentMutationError, CreateDeploymentMutationVariables>): UseMutationResult<CreateDeploymentMutationData, CreateDeploymentMutationError, CreateDeploymentMutationVariables>;
export declare function mutationKeyCreateDeployment(): MutationKey;
export declare function buildCreateDeploymentMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateDeploymentMutationVariables) => Promise<CreateDeploymentMutationData>;
};
//# sourceMappingURL=createDeployment.d.ts.map