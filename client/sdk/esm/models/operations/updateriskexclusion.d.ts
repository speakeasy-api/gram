import * as z from "zod/v4-mini";
import { UpdateRiskExclusionRequestBody, UpdateRiskExclusionRequestBody$Outbound } from "../components/updateriskexclusionrequestbody.js";
export type UpdateRiskExclusionSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UpdateRiskExclusionSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UpdateRiskExclusionSecurity = {
    option1?: UpdateRiskExclusionSecurityOption1 | undefined;
    option2?: UpdateRiskExclusionSecurityOption2 | undefined;
};
export type UpdateRiskExclusionRequest = {
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
    updateRiskExclusionRequestBody: UpdateRiskExclusionRequestBody;
};
/** @internal */
export type UpdateRiskExclusionSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateRiskExclusionSecurityOption1$outboundSchema: z.ZodMiniType<UpdateRiskExclusionSecurityOption1$Outbound, UpdateRiskExclusionSecurityOption1>;
export declare function updateRiskExclusionSecurityOption1ToJSON(updateRiskExclusionSecurityOption1: UpdateRiskExclusionSecurityOption1): string;
/** @internal */
export type UpdateRiskExclusionSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateRiskExclusionSecurityOption2$outboundSchema: z.ZodMiniType<UpdateRiskExclusionSecurityOption2$Outbound, UpdateRiskExclusionSecurityOption2>;
export declare function updateRiskExclusionSecurityOption2ToJSON(updateRiskExclusionSecurityOption2: UpdateRiskExclusionSecurityOption2): string;
/** @internal */
export type UpdateRiskExclusionSecurity$Outbound = {
    Option1?: UpdateRiskExclusionSecurityOption1$Outbound | undefined;
    Option2?: UpdateRiskExclusionSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateRiskExclusionSecurity$outboundSchema: z.ZodMiniType<UpdateRiskExclusionSecurity$Outbound, UpdateRiskExclusionSecurity>;
export declare function updateRiskExclusionSecurityToJSON(updateRiskExclusionSecurity: UpdateRiskExclusionSecurity): string;
/** @internal */
export type UpdateRiskExclusionRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpdateRiskExclusionRequestBody: UpdateRiskExclusionRequestBody$Outbound;
};
/** @internal */
export declare const UpdateRiskExclusionRequest$outboundSchema: z.ZodMiniType<UpdateRiskExclusionRequest$Outbound, UpdateRiskExclusionRequest>;
export declare function updateRiskExclusionRequestToJSON(updateRiskExclusionRequest: UpdateRiskExclusionRequest): string;
//# sourceMappingURL=updateriskexclusion.d.ts.map