import * as z from "zod/v4-mini";
export type SetUserSessionIssuerRequestBody = {
    /**
     * The user_session_issuer id to link, or null to unlink.
     */
    userSessionIssuerId?: string | undefined;
};
/** @internal */
export type SetUserSessionIssuerRequestBody$Outbound = {
    user_session_issuer_id?: string | undefined;
};
/** @internal */
export declare const SetUserSessionIssuerRequestBody$outboundSchema: z.ZodMiniType<SetUserSessionIssuerRequestBody$Outbound, SetUserSessionIssuerRequestBody>;
export declare function setUserSessionIssuerRequestBodyToJSON(setUserSessionIssuerRequestBody: SetUserSessionIssuerRequestBody): string;
//# sourceMappingURL=setusersessionissuerrequestbody.d.ts.map