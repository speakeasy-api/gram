import * as z from "zod/v4-mini";
export type CreatePortalSessionSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type CreatePortalSessionRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type CreatePortalSessionSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreatePortalSessionSecurity$outboundSchema: z.ZodMiniType<CreatePortalSessionSecurity$Outbound, CreatePortalSessionSecurity>;
export declare function createPortalSessionSecurityToJSON(createPortalSessionSecurity: CreatePortalSessionSecurity): string;
/** @internal */
export type CreatePortalSessionRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreatePortalSessionRequest$outboundSchema: z.ZodMiniType<CreatePortalSessionRequest$Outbound, CreatePortalSessionRequest>;
export declare function createPortalSessionRequestToJSON(createPortalSessionRequest: CreatePortalSessionRequest): string;
//# sourceMappingURL=createportalsession.d.ts.map