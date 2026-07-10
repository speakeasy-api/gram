import * as z from "zod/v4-mini";
export type EnableWebhooksSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type EnableWebhooksRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type EnableWebhooksSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const EnableWebhooksSecurity$outboundSchema: z.ZodMiniType<EnableWebhooksSecurity$Outbound, EnableWebhooksSecurity>;
export declare function enableWebhooksSecurityToJSON(enableWebhooksSecurity: EnableWebhooksSecurity): string;
/** @internal */
export type EnableWebhooksRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const EnableWebhooksRequest$outboundSchema: z.ZodMiniType<EnableWebhooksRequest$Outbound, EnableWebhooksRequest>;
export declare function enableWebhooksRequestToJSON(enableWebhooksRequest: EnableWebhooksRequest): string;
//# sourceMappingURL=enablewebhooks.d.ts.map