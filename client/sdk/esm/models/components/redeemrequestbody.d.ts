import * as z from "zod/v4-mini";
export type RedeemRequestBody = {
    /**
     * The opaque one-time code issued by authorize.
     */
    code: string;
    /**
     * The PKCE code verifier whose base64url(sha256(...)) equals the stored code_challenge.
     */
    codeVerifier: string;
};
/** @internal */
export type RedeemRequestBody$Outbound = {
    code: string;
    code_verifier: string;
};
/** @internal */
export declare const RedeemRequestBody$outboundSchema: z.ZodMiniType<RedeemRequestBody$Outbound, RedeemRequestBody>;
export declare function redeemRequestBodyToJSON(redeemRequestBody: RedeemRequestBody): string;
//# sourceMappingURL=redeemrequestbody.d.ts.map