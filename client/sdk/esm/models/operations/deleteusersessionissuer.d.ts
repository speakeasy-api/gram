import * as z from "zod/v4-mini";
export type DeleteUserSessionIssuerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type DeleteUserSessionIssuerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type DeleteUserSessionIssuerSecurity = {
    option1?: DeleteUserSessionIssuerSecurityOption1 | undefined;
    option2?: DeleteUserSessionIssuerSecurityOption2 | undefined;
};
export type DeleteUserSessionIssuerRequest = {
    /**
     * The user_session_issuer id.
     */
    id: string;
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
};
/** @internal */
export type DeleteUserSessionIssuerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteUserSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<DeleteUserSessionIssuerSecurityOption1$Outbound, DeleteUserSessionIssuerSecurityOption1>;
export declare function deleteUserSessionIssuerSecurityOption1ToJSON(deleteUserSessionIssuerSecurityOption1: DeleteUserSessionIssuerSecurityOption1): string;
/** @internal */
export type DeleteUserSessionIssuerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteUserSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<DeleteUserSessionIssuerSecurityOption2$Outbound, DeleteUserSessionIssuerSecurityOption2>;
export declare function deleteUserSessionIssuerSecurityOption2ToJSON(deleteUserSessionIssuerSecurityOption2: DeleteUserSessionIssuerSecurityOption2): string;
/** @internal */
export type DeleteUserSessionIssuerSecurity$Outbound = {
    Option1?: DeleteUserSessionIssuerSecurityOption1$Outbound | undefined;
    Option2?: DeleteUserSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteUserSessionIssuerSecurity$outboundSchema: z.ZodMiniType<DeleteUserSessionIssuerSecurity$Outbound, DeleteUserSessionIssuerSecurity>;
export declare function deleteUserSessionIssuerSecurityToJSON(deleteUserSessionIssuerSecurity: DeleteUserSessionIssuerSecurity): string;
/** @internal */
export type DeleteUserSessionIssuerRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteUserSessionIssuerRequest$outboundSchema: z.ZodMiniType<DeleteUserSessionIssuerRequest$Outbound, DeleteUserSessionIssuerRequest>;
export declare function deleteUserSessionIssuerRequestToJSON(deleteUserSessionIssuerRequest: DeleteUserSessionIssuerRequest): string;
//# sourceMappingURL=deleteusersessionissuer.d.ts.map