import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicyBypassRequest } from "../models/components/riskpolicybypassrequest.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateRiskPolicyBypassRequestRequest, CreateRiskPolicyBypassRequestSecurity } from "../models/operations/createriskpolicybypassrequest.js";
import { MutationHookOptions } from "./_types.js";
export type RiskCreatePolicyBypassRequestMutationVariables = {
    request: CreateRiskPolicyBypassRequestRequest;
    security?: CreateRiskPolicyBypassRequestSecurity | undefined;
    options?: RequestOptions;
};
export type RiskCreatePolicyBypassRequestMutationData = RiskPolicyBypassRequest;
export type RiskCreatePolicyBypassRequestMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createRiskPolicyBypassRequest risk
 *
 * @remarks
 * Create or refresh a risk policy bypass request from a signed request URL token.
 */
export declare function useRiskCreatePolicyBypassRequestMutation(options?: MutationHookOptions<RiskCreatePolicyBypassRequestMutationData, RiskCreatePolicyBypassRequestMutationError, RiskCreatePolicyBypassRequestMutationVariables>): UseMutationResult<RiskCreatePolicyBypassRequestMutationData, RiskCreatePolicyBypassRequestMutationError, RiskCreatePolicyBypassRequestMutationVariables>;
export declare function mutationKeyRiskCreatePolicyBypassRequest(): MutationKey;
export declare function buildRiskCreatePolicyBypassRequestMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskCreatePolicyBypassRequestMutationVariables) => Promise<RiskCreatePolicyBypassRequestMutationData>;
};
//# sourceMappingURL=riskCreatePolicyBypassRequest.d.ts.map