import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicyBypassRequest } from "../models/components/riskpolicybypassrequest.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DenyRiskPolicyBypassRequestRequest, DenyRiskPolicyBypassRequestSecurity } from "../models/operations/denyriskpolicybypassrequest.js";
import { MutationHookOptions } from "./_types.js";
export type RiskDenyPolicyBypassRequestMutationVariables = {
    request: DenyRiskPolicyBypassRequestRequest;
    security?: DenyRiskPolicyBypassRequestSecurity | undefined;
    options?: RequestOptions;
};
export type RiskDenyPolicyBypassRequestMutationData = RiskPolicyBypassRequest;
export type RiskDenyPolicyBypassRequestMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * denyRiskPolicyBypassRequest risk
 *
 * @remarks
 * Deny a risk policy bypass request, updating workflow state.
 */
export declare function useRiskDenyPolicyBypassRequestMutation(options?: MutationHookOptions<RiskDenyPolicyBypassRequestMutationData, RiskDenyPolicyBypassRequestMutationError, RiskDenyPolicyBypassRequestMutationVariables>): UseMutationResult<RiskDenyPolicyBypassRequestMutationData, RiskDenyPolicyBypassRequestMutationError, RiskDenyPolicyBypassRequestMutationVariables>;
export declare function mutationKeyRiskDenyPolicyBypassRequest(): MutationKey;
export declare function buildRiskDenyPolicyBypassRequestMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskDenyPolicyBypassRequestMutationVariables) => Promise<RiskDenyPolicyBypassRequestMutationData>;
};
//# sourceMappingURL=riskDenyPolicyBypassRequest.d.ts.map