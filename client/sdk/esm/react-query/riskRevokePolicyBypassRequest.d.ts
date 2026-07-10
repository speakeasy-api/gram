import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicyBypassRequest } from "../models/components/riskpolicybypassrequest.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RevokeRiskPolicyBypassRequestRequest, RevokeRiskPolicyBypassRequestSecurity } from "../models/operations/revokeriskpolicybypassrequest.js";
import { MutationHookOptions } from "./_types.js";
export type RiskRevokePolicyBypassRequestMutationVariables = {
    request: RevokeRiskPolicyBypassRequestRequest;
    security?: RevokeRiskPolicyBypassRequestSecurity | undefined;
    options?: RequestOptions;
};
export type RiskRevokePolicyBypassRequestMutationData = RiskPolicyBypassRequest;
export type RiskRevokePolicyBypassRequestMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * revokeRiskPolicyBypassRequest risk
 *
 * @remarks
 * Revoke a previously approved risk policy bypass request.
 */
export declare function useRiskRevokePolicyBypassRequestMutation(options?: MutationHookOptions<RiskRevokePolicyBypassRequestMutationData, RiskRevokePolicyBypassRequestMutationError, RiskRevokePolicyBypassRequestMutationVariables>): UseMutationResult<RiskRevokePolicyBypassRequestMutationData, RiskRevokePolicyBypassRequestMutationError, RiskRevokePolicyBypassRequestMutationVariables>;
export declare function mutationKeyRiskRevokePolicyBypassRequest(): MutationKey;
export declare function buildRiskRevokePolicyBypassRequestMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskRevokePolicyBypassRequestMutationVariables) => Promise<RiskRevokePolicyBypassRequestMutationData>;
};
//# sourceMappingURL=riskRevokePolicyBypassRequest.d.ts.map