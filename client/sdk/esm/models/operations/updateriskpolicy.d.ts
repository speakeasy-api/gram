import * as z from "zod/v4-mini";
import { UpdateRiskPolicyRequestBody, UpdateRiskPolicyRequestBody$Outbound } from "../components/updateriskpolicyrequestbody.js";
export type UpdateRiskPolicySecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UpdateRiskPolicySecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UpdateRiskPolicySecurity = {
    option1?: UpdateRiskPolicySecurityOption1 | undefined;
    option2?: UpdateRiskPolicySecurityOption2 | undefined;
};
export type UpdateRiskPolicyRequest = {
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
    updateRiskPolicyRequestBody: UpdateRiskPolicyRequestBody;
};
/** @internal */
export type UpdateRiskPolicySecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateRiskPolicySecurityOption1$outboundSchema: z.ZodMiniType<UpdateRiskPolicySecurityOption1$Outbound, UpdateRiskPolicySecurityOption1>;
export declare function updateRiskPolicySecurityOption1ToJSON(updateRiskPolicySecurityOption1: UpdateRiskPolicySecurityOption1): string;
/** @internal */
export type UpdateRiskPolicySecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateRiskPolicySecurityOption2$outboundSchema: z.ZodMiniType<UpdateRiskPolicySecurityOption2$Outbound, UpdateRiskPolicySecurityOption2>;
export declare function updateRiskPolicySecurityOption2ToJSON(updateRiskPolicySecurityOption2: UpdateRiskPolicySecurityOption2): string;
/** @internal */
export type UpdateRiskPolicySecurity$Outbound = {
    Option1?: UpdateRiskPolicySecurityOption1$Outbound | undefined;
    Option2?: UpdateRiskPolicySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateRiskPolicySecurity$outboundSchema: z.ZodMiniType<UpdateRiskPolicySecurity$Outbound, UpdateRiskPolicySecurity>;
export declare function updateRiskPolicySecurityToJSON(updateRiskPolicySecurity: UpdateRiskPolicySecurity): string;
/** @internal */
export type UpdateRiskPolicyRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpdateRiskPolicyRequestBody: UpdateRiskPolicyRequestBody$Outbound;
};
/** @internal */
export declare const UpdateRiskPolicyRequest$outboundSchema: z.ZodMiniType<UpdateRiskPolicyRequest$Outbound, UpdateRiskPolicyRequest>;
export declare function updateRiskPolicyRequestToJSON(updateRiskPolicyRequest: UpdateRiskPolicyRequest): string;
//# sourceMappingURL=updateriskpolicy.d.ts.map