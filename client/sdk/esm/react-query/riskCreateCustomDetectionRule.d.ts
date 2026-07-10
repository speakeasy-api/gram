import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskCustomDetectionRule } from "../models/components/riskcustomdetectionrule.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateCustomDetectionRuleRequest, CreateCustomDetectionRuleSecurity } from "../models/operations/createcustomdetectionrule.js";
import { MutationHookOptions } from "./_types.js";
export type RiskCreateCustomDetectionRuleMutationVariables = {
    request: CreateCustomDetectionRuleRequest;
    security?: CreateCustomDetectionRuleSecurity | undefined;
    options?: RequestOptions;
};
export type RiskCreateCustomDetectionRuleMutationData = RiskCustomDetectionRule;
export type RiskCreateCustomDetectionRuleMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createCustomDetectionRule risk
 *
 * @remarks
 * Create a custom regex-backed detection rule for the current project.
 */
export declare function useRiskCreateCustomDetectionRuleMutation(options?: MutationHookOptions<RiskCreateCustomDetectionRuleMutationData, RiskCreateCustomDetectionRuleMutationError, RiskCreateCustomDetectionRuleMutationVariables>): UseMutationResult<RiskCreateCustomDetectionRuleMutationData, RiskCreateCustomDetectionRuleMutationError, RiskCreateCustomDetectionRuleMutationVariables>;
export declare function mutationKeyRiskCreateCustomDetectionRule(): MutationKey;
export declare function buildRiskCreateCustomDetectionRuleMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskCreateCustomDetectionRuleMutationVariables) => Promise<RiskCreateCustomDetectionRuleMutationData>;
};
//# sourceMappingURL=riskCreateCustomDetectionRule.d.ts.map