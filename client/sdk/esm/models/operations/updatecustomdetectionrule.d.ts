import * as z from "zod/v4-mini";
import { UpdateCustomDetectionRuleRequestBody, UpdateCustomDetectionRuleRequestBody$Outbound } from "../components/updatecustomdetectionrulerequestbody.js";
export type UpdateCustomDetectionRuleSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UpdateCustomDetectionRuleSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UpdateCustomDetectionRuleSecurity = {
    option1?: UpdateCustomDetectionRuleSecurityOption1 | undefined;
    option2?: UpdateCustomDetectionRuleSecurityOption2 | undefined;
};
export type UpdateCustomDetectionRuleRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    updateCustomDetectionRuleRequestBody: UpdateCustomDetectionRuleRequestBody;
};
/** @internal */
export type UpdateCustomDetectionRuleSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateCustomDetectionRuleSecurityOption1$outboundSchema: z.ZodMiniType<UpdateCustomDetectionRuleSecurityOption1$Outbound, UpdateCustomDetectionRuleSecurityOption1>;
export declare function updateCustomDetectionRuleSecurityOption1ToJSON(updateCustomDetectionRuleSecurityOption1: UpdateCustomDetectionRuleSecurityOption1): string;
/** @internal */
export type UpdateCustomDetectionRuleSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateCustomDetectionRuleSecurityOption2$outboundSchema: z.ZodMiniType<UpdateCustomDetectionRuleSecurityOption2$Outbound, UpdateCustomDetectionRuleSecurityOption2>;
export declare function updateCustomDetectionRuleSecurityOption2ToJSON(updateCustomDetectionRuleSecurityOption2: UpdateCustomDetectionRuleSecurityOption2): string;
/** @internal */
export type UpdateCustomDetectionRuleSecurity$Outbound = {
    Option1?: UpdateCustomDetectionRuleSecurityOption1$Outbound | undefined;
    Option2?: UpdateCustomDetectionRuleSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateCustomDetectionRuleSecurity$outboundSchema: z.ZodMiniType<UpdateCustomDetectionRuleSecurity$Outbound, UpdateCustomDetectionRuleSecurity>;
export declare function updateCustomDetectionRuleSecurityToJSON(updateCustomDetectionRuleSecurity: UpdateCustomDetectionRuleSecurity): string;
/** @internal */
export type UpdateCustomDetectionRuleRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpdateCustomDetectionRuleRequestBody: UpdateCustomDetectionRuleRequestBody$Outbound;
};
/** @internal */
export declare const UpdateCustomDetectionRuleRequest$outboundSchema: z.ZodMiniType<UpdateCustomDetectionRuleRequest$Outbound, UpdateCustomDetectionRuleRequest>;
export declare function updateCustomDetectionRuleRequestToJSON(updateCustomDetectionRuleRequest: UpdateCustomDetectionRuleRequest): string;
//# sourceMappingURL=updatecustomdetectionrule.d.ts.map