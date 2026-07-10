import {
  InvalidateQueryFilters,
  QueryClient,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  EvaluatePromptGuardrailRequest,
  EvaluatePromptGuardrailSecurity,
} from "../models/operations/evaluatepromptguardrail.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskEvaluatePromptGuardrailQuery,
  prefetchRiskEvaluatePromptGuardrail,
  queryKeyRiskEvaluatePromptGuardrail,
  RiskEvaluatePromptGuardrailQueryData,
} from "./riskEvaluatePromptGuardrail.core.js";
export {
  buildRiskEvaluatePromptGuardrailQuery,
  prefetchRiskEvaluatePromptGuardrail,
  queryKeyRiskEvaluatePromptGuardrail,
  type RiskEvaluatePromptGuardrailQueryData,
};
export type RiskEvaluatePromptGuardrailQueryError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * evaluatePromptGuardrail risk
 *
 * @remarks
 * Replay a prompt_based guardrail against a single chat session and return the LLM judge's per-message verdict. The guardrail (prompt + judge config + message-type scope + CEL scope) is passed inline so the policy-eval workbench can evaluate an unsaved draft before a policy exists. This path is read-only: it never writes risk_results, publishes to the outbox, or enforces. It exists purely to tune a guardrail against real transcripts. Judges only the chat's latest generation; message-type scoping and CEL scope predicates are both applied.
 */
export declare function useRiskEvaluatePromptGuardrail(
  request: EvaluatePromptGuardrailRequest,
  security?: EvaluatePromptGuardrailSecurity | undefined,
  options?: QueryHookOptions<
    RiskEvaluatePromptGuardrailQueryData,
    RiskEvaluatePromptGuardrailQueryError
  >,
): UseQueryResult<
  RiskEvaluatePromptGuardrailQueryData,
  RiskEvaluatePromptGuardrailQueryError
>;
/**
 * evaluatePromptGuardrail risk
 *
 * @remarks
 * Replay a prompt_based guardrail against a single chat session and return the LLM judge's per-message verdict. The guardrail (prompt + judge config + message-type scope + CEL scope) is passed inline so the policy-eval workbench can evaluate an unsaved draft before a policy exists. This path is read-only: it never writes risk_results, publishes to the outbox, or enforces. It exists purely to tune a guardrail against real transcripts. Judges only the chat's latest generation; message-type scoping and CEL scope predicates are both applied.
 */
export declare function useRiskEvaluatePromptGuardrailSuspense(
  request: EvaluatePromptGuardrailRequest,
  security?: EvaluatePromptGuardrailSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskEvaluatePromptGuardrailQueryData,
    RiskEvaluatePromptGuardrailQueryError
  >,
): UseSuspenseQueryResult<
  RiskEvaluatePromptGuardrailQueryData,
  RiskEvaluatePromptGuardrailQueryError
>;
export declare function setRiskEvaluatePromptGuardrailData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskEvaluatePromptGuardrailQueryData,
): RiskEvaluatePromptGuardrailQueryData | undefined;
export declare function invalidateRiskEvaluatePromptGuardrail(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRiskEvaluatePromptGuardrail(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskEvaluatePromptGuardrail.d.ts.map
