import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteRiskExclusionRequest, DeleteRiskExclusionSecurity } from "../models/operations/deleteriskexclusion.js";
import { MutationHookOptions } from "./_types.js";
export type RiskDeleteExclusionMutationVariables = {
    request: DeleteRiskExclusionRequest;
    security?: DeleteRiskExclusionSecurity | undefined;
    options?: RequestOptions;
};
export type RiskDeleteExclusionMutationData = void;
export type RiskDeleteExclusionMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteRiskExclusion risk
 *
 * @remarks
 * Delete a risk exclusion. Previously suppressed findings are restored.
 */
export declare function useRiskDeleteExclusionMutation(options?: MutationHookOptions<RiskDeleteExclusionMutationData, RiskDeleteExclusionMutationError, RiskDeleteExclusionMutationVariables>): UseMutationResult<RiskDeleteExclusionMutationData, RiskDeleteExclusionMutationError, RiskDeleteExclusionMutationVariables>;
export declare function mutationKeyRiskDeleteExclusion(): MutationKey;
export declare function buildRiskDeleteExclusionMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskDeleteExclusionMutationVariables) => Promise<RiskDeleteExclusionMutationData>;
};
//# sourceMappingURL=riskDeleteExclusion.d.ts.map