import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PromptGuardrailEvalResult } from "../models/components/promptguardrailevalresult.js";
import {
  EvaluatePromptGuardrailRequest,
  EvaluatePromptGuardrailSecurity,
} from "../models/operations/evaluatepromptguardrail.js";
export type RiskEvaluatePromptGuardrailQueryData = PromptGuardrailEvalResult;
export declare function prefetchRiskEvaluatePromptGuardrail(
  queryClient: QueryClient,
  client$: GramCore,
  request: EvaluatePromptGuardrailRequest,
  security?: EvaluatePromptGuardrailSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRiskEvaluatePromptGuardrailQuery(
  client$: GramCore,
  request: EvaluatePromptGuardrailRequest,
  security?: EvaluatePromptGuardrailSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<RiskEvaluatePromptGuardrailQueryData>;
};
export declare function queryKeyRiskEvaluatePromptGuardrail(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskEvaluatePromptGuardrail.core.d.ts.map
