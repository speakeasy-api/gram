import * as z from "zod/v4-mini";
import {
  RiskPolicyModelConfig,
  RiskPolicyModelConfig$Outbound,
} from "./riskpolicymodelconfig.js";
export type EvaluatePromptGuardrailRequestBody = {
  /**
   * The chat session to replay the guardrail against.
   */
  chatId: string;
  /**
   * Message types to judge (user_message, assistant_message, tool_request, tool_response), matching a policy's message_types. When empty or omitted, judges all supported types.
   */
  messageTypes?: Array<string> | undefined;
  modelConfig?: RiskPolicyModelConfig | undefined;
  /**
   * The guardrail prompt the LLM judge evaluates each in-scope message against.
   */
  prompt: string;
  /**
   * CEL exemption predicate: the replay skips a message when this boolean expression is true. Omit/empty means no inline exemption.
   */
  scopeExempt?: string | undefined;
  /**
   * CEL scope predicate: the replay judges a message only when this boolean expression is true (in addition to message_types). Omit/empty means all messages are in scope.
   */
  scopeInclude?: string | undefined;
};
/** @internal */
export type EvaluatePromptGuardrailRequestBody$Outbound = {
  chat_id: string;
  message_types?: Array<string> | undefined;
  model_config?: RiskPolicyModelConfig$Outbound | undefined;
  prompt: string;
  scope_exempt?: string | undefined;
  scope_include?: string | undefined;
};
/** @internal */
export declare const EvaluatePromptGuardrailRequestBody$outboundSchema: z.ZodMiniType<
  EvaluatePromptGuardrailRequestBody$Outbound,
  EvaluatePromptGuardrailRequestBody
>;
export declare function evaluatePromptGuardrailRequestBodyToJSON(
  evaluatePromptGuardrailRequestBody: EvaluatePromptGuardrailRequestBody,
): string;
//# sourceMappingURL=evaluatepromptguardrailrequestbody.d.ts.map
