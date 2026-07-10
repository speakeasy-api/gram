import * as z from "zod/v4-mini";
export type SuggestCustomDetectionRuleRequestBody = {
    /**
     * Existing built-in and custom rule ids the suggested id must avoid colliding with.
     */
    existingRuleIds?: Array<string> | undefined;
    /**
     * Natural-language description of what the rule should detect.
     */
    prompt: string;
};
/** @internal */
export type SuggestCustomDetectionRuleRequestBody$Outbound = {
    existing_rule_ids?: Array<string> | undefined;
    prompt: string;
};
/** @internal */
export declare const SuggestCustomDetectionRuleRequestBody$outboundSchema: z.ZodMiniType<SuggestCustomDetectionRuleRequestBody$Outbound, SuggestCustomDetectionRuleRequestBody>;
export declare function suggestCustomDetectionRuleRequestBodyToJSON(suggestCustomDetectionRuleRequestBody: SuggestCustomDetectionRuleRequestBody): string;
//# sourceMappingURL=suggestcustomdetectionrulerequestbody.d.ts.map