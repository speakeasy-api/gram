import * as z from "zod/v4-mini";
export type MoveIssuerRequestBody = {
    /**
     * The remote_session_issuer id.
     */
    id: string;
    /**
     * Target owning project id; the project must belong to the caller's organization. Omit to make the issuer organization-level.
     */
    projectId?: string | undefined;
};
/** @internal */
export type MoveIssuerRequestBody$Outbound = {
    id: string;
    project_id?: string | undefined;
};
/** @internal */
export declare const MoveIssuerRequestBody$outboundSchema: z.ZodMiniType<MoveIssuerRequestBody$Outbound, MoveIssuerRequestBody>;
export declare function moveIssuerRequestBodyToJSON(moveIssuerRequestBody: MoveIssuerRequestBody): string;
//# sourceMappingURL=moveissuerrequestbody.d.ts.map