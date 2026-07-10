import * as z from "zod/v4-mini";
/**
 * Form for attaching a user_session_issuer to a remote_session_client via the join table.
 */
export type AttachUserSessionIssuerForm = {
    /**
     * The remote_session_client id.
     */
    id: string;
    /**
     * The user_session_issuer to attach.
     */
    userSessionIssuerId: string;
};
/** @internal */
export type AttachUserSessionIssuerForm$Outbound = {
    id: string;
    user_session_issuer_id: string;
};
/** @internal */
export declare const AttachUserSessionIssuerForm$outboundSchema: z.ZodMiniType<AttachUserSessionIssuerForm$Outbound, AttachUserSessionIssuerForm>;
export declare function attachUserSessionIssuerFormToJSON(attachUserSessionIssuerForm: AttachUserSessionIssuerForm): string;
//# sourceMappingURL=attachusersessionissuerform.d.ts.map