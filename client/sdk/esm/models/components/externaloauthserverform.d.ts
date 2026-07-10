import * as z from "zod/v4-mini";
export type ExternalOAuthServerForm = {
    /**
     * The metadata for the external OAuth server
     */
    metadata: any;
    /**
     * A short url-friendly label that uniquely identifies a resource.
     */
    slug: string;
};
/** @internal */
export type ExternalOAuthServerForm$Outbound = {
    metadata: any;
    slug: string;
};
/** @internal */
export declare const ExternalOAuthServerForm$outboundSchema: z.ZodMiniType<ExternalOAuthServerForm$Outbound, ExternalOAuthServerForm>;
export declare function externalOAuthServerFormToJSON(externalOAuthServerForm: ExternalOAuthServerForm): string;
//# sourceMappingURL=externaloauthserverform.d.ts.map