import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RedeployResult } from "../models/components/redeployresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RedeployDeploymentRequest, RedeployDeploymentSecurity } from "../models/operations/redeploydeployment.js";
import { MutationHookOptions } from "./_types.js";
export type RedeployDeploymentMutationVariables = {
    request: RedeployDeploymentRequest;
    security?: RedeployDeploymentSecurity | undefined;
    options?: RequestOptions;
};
export type RedeployDeploymentMutationData = RedeployResult;
export type RedeployDeploymentMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * redeploy deployments
 *
 * @remarks
 * Redeploys an existing deployment.
 */
export declare function useRedeployDeploymentMutation(options?: MutationHookOptions<RedeployDeploymentMutationData, RedeployDeploymentMutationError, RedeployDeploymentMutationVariables>): UseMutationResult<RedeployDeploymentMutationData, RedeployDeploymentMutationError, RedeployDeploymentMutationVariables>;
export declare function mutationKeyRedeployDeployment(): MutationKey;
export declare function buildRedeployDeploymentMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RedeployDeploymentMutationVariables) => Promise<RedeployDeploymentMutationData>;
};
//# sourceMappingURL=redeployDeployment.d.ts.map