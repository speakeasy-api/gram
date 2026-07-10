import * as z from "zod/v4-mini";
import { EvaluatePromptGuardrailRequestBody, EvaluatePromptGuardrailRequestBody$Outbound } from "../components/evaluatepromptguardrailrequestbody.js";
export type EvaluatePromptGuardrailSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type EvaluatePromptGuardrailSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type EvaluatePromptGuardrailSecurity = {
    option1?: EvaluatePromptGuardrailSecurityOption1 | undefined;
    option2?: EvaluatePromptGuardrailSecurityOption2 | undefined;
};
export type EvaluatePromptGuardrailRequest = {
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
    evaluatePromptGuardrailRequestBody: EvaluatePromptGuardrailRequestBody;
};
/** @internal */
export type EvaluatePromptGuardrailSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const EvaluatePromptGuardrailSecurityOption1$outboundSchema: z.ZodMiniType<EvaluatePromptGuardrailSecurityOption1$Outbound, EvaluatePromptGuardrailSecurityOption1>;
export declare function evaluatePromptGuardrailSecurityOption1ToJSON(evaluatePromptGuardrailSecurityOption1: EvaluatePromptGuardrailSecurityOption1): string;
/** @internal */
export type EvaluatePromptGuardrailSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const EvaluatePromptGuardrailSecurityOption2$outboundSchema: z.ZodMiniType<EvaluatePromptGuardrailSecurityOption2$Outbound, EvaluatePromptGuardrailSecurityOption2>;
export declare function evaluatePromptGuardrailSecurityOption2ToJSON(evaluatePromptGuardrailSecurityOption2: EvaluatePromptGuardrailSecurityOption2): string;
/** @internal */
export type EvaluatePromptGuardrailSecurity$Outbound = {
    Option1?: EvaluatePromptGuardrailSecurityOption1$Outbound | undefined;
    Option2?: EvaluatePromptGuardrailSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const EvaluatePromptGuardrailSecurity$outboundSchema: z.ZodMiniType<EvaluatePromptGuardrailSecurity$Outbound, EvaluatePromptGuardrailSecurity>;
export declare function evaluatePromptGuardrailSecurityToJSON(evaluatePromptGuardrailSecurity: EvaluatePromptGuardrailSecurity): string;
/** @internal */
export type EvaluatePromptGuardrailRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    EvaluatePromptGuardrailRequestBody: EvaluatePromptGuardrailRequestBody$Outbound;
};
/** @internal */
export declare const EvaluatePromptGuardrailRequest$outboundSchema: z.ZodMiniType<EvaluatePromptGuardrailRequest$Outbound, EvaluatePromptGuardrailRequest>;
export declare function evaluatePromptGuardrailRequestToJSON(evaluatePromptGuardrailRequest: EvaluatePromptGuardrailRequest): string;
//# sourceMappingURL=evaluatepromptguardrail.d.ts.map