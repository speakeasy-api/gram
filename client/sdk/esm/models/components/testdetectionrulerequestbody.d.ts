import * as z from "zod/v4-mini";
export type TestDetectionRuleRequestBody = {
  /**
   * CEL detection predicate for `custom.*` rule ids, evaluated against the sample message.
   */
  detectionExpr?: string | undefined;
  /**
   * Rule identifier to evaluate (e.g. `secret.aws_access_token`, `pii.email_address`, `custom.acme_token`).
   */
  ruleId: string;
  /**
   * Sample text to scan.
   */
  text: string;
};
/** @internal */
export type TestDetectionRuleRequestBody$Outbound = {
  detection_expr?: string | undefined;
  rule_id: string;
  text: string;
};
/** @internal */
export declare const TestDetectionRuleRequestBody$outboundSchema: z.ZodMiniType<
  TestDetectionRuleRequestBody$Outbound,
  TestDetectionRuleRequestBody
>;
export declare function testDetectionRuleRequestBodyToJSON(
  testDetectionRuleRequestBody: TestDetectionRuleRequestBody,
): string;
//# sourceMappingURL=testdetectionrulerequestbody.d.ts.map
