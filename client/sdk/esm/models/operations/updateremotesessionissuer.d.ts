import * as z from "zod/v4-mini";
import { UpdateRemoteSessionIssuerForm, UpdateRemoteSessionIssuerForm$Outbound } from "../components/updateremotesessionissuerform.js";
export type UpdateRemoteSessionIssuerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UpdateRemoteSessionIssuerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UpdateRemoteSessionIssuerSecurity = {
    option1?: UpdateRemoteSessionIssuerSecurityOption1 | undefined;
    option2?: UpdateRemoteSessionIssuerSecurityOption2 | undefined;
};
export type UpdateRemoteSessionIssuerRequest = {
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
    updateRemoteSessionIssuerForm: UpdateRemoteSessionIssuerForm;
};
/** @internal */
export type UpdateRemoteSessionIssuerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateRemoteSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<UpdateRemoteSessionIssuerSecurityOption1$Outbound, UpdateRemoteSessionIssuerSecurityOption1>;
export declare function updateRemoteSessionIssuerSecurityOption1ToJSON(updateRemoteSessionIssuerSecurityOption1: UpdateRemoteSessionIssuerSecurityOption1): string;
/** @internal */
export type UpdateRemoteSessionIssuerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateRemoteSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<UpdateRemoteSessionIssuerSecurityOption2$Outbound, UpdateRemoteSessionIssuerSecurityOption2>;
export declare function updateRemoteSessionIssuerSecurityOption2ToJSON(updateRemoteSessionIssuerSecurityOption2: UpdateRemoteSessionIssuerSecurityOption2): string;
/** @internal */
export type UpdateRemoteSessionIssuerSecurity$Outbound = {
    Option1?: UpdateRemoteSessionIssuerSecurityOption1$Outbound | undefined;
    Option2?: UpdateRemoteSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<UpdateRemoteSessionIssuerSecurity$Outbound, UpdateRemoteSessionIssuerSecurity>;
export declare function updateRemoteSessionIssuerSecurityToJSON(updateRemoteSessionIssuerSecurity: UpdateRemoteSessionIssuerSecurity): string;
/** @internal */
export type UpdateRemoteSessionIssuerRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpdateRemoteSessionIssuerForm: UpdateRemoteSessionIssuerForm$Outbound;
};
/** @internal */
export declare const UpdateRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<UpdateRemoteSessionIssuerRequest$Outbound, UpdateRemoteSessionIssuerRequest>;
export declare function updateRemoteSessionIssuerRequestToJSON(updateRemoteSessionIssuerRequest: UpdateRemoteSessionIssuerRequest): string;
//# sourceMappingURL=updateremotesessionissuer.d.ts.map