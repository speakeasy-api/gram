import * as z from "zod/v4-mini";
import { UpdateUserSessionIssuerForm, UpdateUserSessionIssuerForm$Outbound } from "../components/updateusersessionissuerform.js";
export type UpdateUserSessionIssuerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UpdateUserSessionIssuerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UpdateUserSessionIssuerSecurity = {
    option1?: UpdateUserSessionIssuerSecurityOption1 | undefined;
    option2?: UpdateUserSessionIssuerSecurityOption2 | undefined;
};
export type UpdateUserSessionIssuerRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    updateUserSessionIssuerForm: UpdateUserSessionIssuerForm;
};
/** @internal */
export type UpdateUserSessionIssuerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateUserSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<UpdateUserSessionIssuerSecurityOption1$Outbound, UpdateUserSessionIssuerSecurityOption1>;
export declare function updateUserSessionIssuerSecurityOption1ToJSON(updateUserSessionIssuerSecurityOption1: UpdateUserSessionIssuerSecurityOption1): string;
/** @internal */
export type UpdateUserSessionIssuerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateUserSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<UpdateUserSessionIssuerSecurityOption2$Outbound, UpdateUserSessionIssuerSecurityOption2>;
export declare function updateUserSessionIssuerSecurityOption2ToJSON(updateUserSessionIssuerSecurityOption2: UpdateUserSessionIssuerSecurityOption2): string;
/** @internal */
export type UpdateUserSessionIssuerSecurity$Outbound = {
    Option1?: UpdateUserSessionIssuerSecurityOption1$Outbound | undefined;
    Option2?: UpdateUserSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateUserSessionIssuerSecurity$outboundSchema: z.ZodMiniType<UpdateUserSessionIssuerSecurity$Outbound, UpdateUserSessionIssuerSecurity>;
export declare function updateUserSessionIssuerSecurityToJSON(updateUserSessionIssuerSecurity: UpdateUserSessionIssuerSecurity): string;
/** @internal */
export type UpdateUserSessionIssuerRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpdateUserSessionIssuerForm: UpdateUserSessionIssuerForm$Outbound;
};
/** @internal */
export declare const UpdateUserSessionIssuerRequest$outboundSchema: z.ZodMiniType<UpdateUserSessionIssuerRequest$Outbound, UpdateUserSessionIssuerRequest>;
export declare function updateUserSessionIssuerRequestToJSON(updateUserSessionIssuerRequest: UpdateUserSessionIssuerRequest): string;
//# sourceMappingURL=updateusersessionissuer.d.ts.map