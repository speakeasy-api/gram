import * as z from "zod/v4-mini";
export type DisableWebhooksSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type DisableWebhooksRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type DisableWebhooksSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DisableWebhooksSecurity$outboundSchema: z.ZodMiniType<DisableWebhooksSecurity$Outbound, DisableWebhooksSecurity>;
export declare function disableWebhooksSecurityToJSON(disableWebhooksSecurity: DisableWebhooksSecurity): string;
/** @internal */
export type DisableWebhooksRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DisableWebhooksRequest$outboundSchema: z.ZodMiniType<DisableWebhooksRequest$Outbound, DisableWebhooksRequest>;
export declare function disableWebhooksRequestToJSON(disableWebhooksRequest: DisableWebhooksRequest): string;
//# sourceMappingURL=disablewebhooks.d.ts.map