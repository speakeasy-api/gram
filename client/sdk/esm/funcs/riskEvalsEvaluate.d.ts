import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PromptGuardrailEvalResult } from "../models/components/promptguardrailevalresult.js";
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
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * evaluatePromptGuardrail risk
 *
 * @remarks
 * Replay a prompt_based guardrail against a single chat session and return the LLM judge's per-message verdict. The guardrail (prompt + judge config + message-type scope + CEL scope) is passed inline so the policy-eval workbench can evaluate an unsaved draft before a policy exists. This path is read-only: it never writes risk_results, publishes to the outbox, or enforces. It exists purely to tune a guardrail against real transcripts. Judges only the chat's latest generation; message-type scoping and CEL scope predicates are both applied.
 */
export declare function riskEvalsEvaluate(
  client: GramCore,
  request: EvaluatePromptGuardrailRequest,
  security?: EvaluatePromptGuardrailSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    PromptGuardrailEvalResult,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=riskEvalsEvaluate.d.ts.map
