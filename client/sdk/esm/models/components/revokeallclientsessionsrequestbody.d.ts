import * as z from "zod/v4-mini";
export type RevokeAllClientSessionsRequestBody = {
    /**
     * The remote_session_client id.
     */
    clientId: string;
};
/** @internal */
export type RevokeAllClientSessionsRequestBody$Outbound = {
    client_id: string;
};
/** @internal */
export declare const RevokeAllClientSessionsRequestBody$outboundSchema: z.ZodMiniType<RevokeAllClientSessionsRequestBody$Outbound, RevokeAllClientSessionsRequestBody>;
export declare function revokeAllClientSessionsRequestBodyToJSON(revokeAllClientSessionsRequestBody: RevokeAllClientSessionsRequestBody): string;
//# sourceMappingURL=revokeallclientsessionsrequestbody.d.ts.map