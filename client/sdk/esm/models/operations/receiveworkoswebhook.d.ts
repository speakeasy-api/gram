import * as z from "zod/v4-mini";
export type ReceiveWorkOSWebhookRequest = {
    /**
     * WorkOS webhook signature header
     */
    workOSSignature?: string | undefined;
};
/** @internal */
export type ReceiveWorkOSWebhookRequest$Outbound = {
    "WorkOS-Signature"?: string | undefined;
};
/** @internal */
export declare const ReceiveWorkOSWebhookRequest$outboundSchema: z.ZodMiniType<ReceiveWorkOSWebhookRequest$Outbound, ReceiveWorkOSWebhookRequest>;
export declare function receiveWorkOSWebhookRequestToJSON(receiveWorkOSWebhookRequest: ReceiveWorkOSWebhookRequest): string;
//# sourceMappingURL=receiveworkoswebhook.d.ts.map