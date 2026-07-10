import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type DeleteOtelForwardingDestinationSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type DeleteOtelForwardingDestinationRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    serveImageForm: components.ServeImageForm;
};
/** @internal */
export type DeleteOtelForwardingDestinationSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteOtelForwardingDestinationSecurity$outboundSchema: z.ZodMiniType<DeleteOtelForwardingDestinationSecurity$Outbound, DeleteOtelForwardingDestinationSecurity>;
export declare function deleteOtelForwardingDestinationSecurityToJSON(deleteOtelForwardingDestinationSecurity: DeleteOtelForwardingDestinationSecurity): string;
/** @internal */
export type DeleteOtelForwardingDestinationRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    ServeImageForm: components.ServeImageForm$Outbound;
};
/** @internal */
export declare const DeleteOtelForwardingDestinationRequest$outboundSchema: z.ZodMiniType<DeleteOtelForwardingDestinationRequest$Outbound, DeleteOtelForwardingDestinationRequest>;
export declare function deleteOtelForwardingDestinationRequestToJSON(deleteOtelForwardingDestinationRequest: DeleteOtelForwardingDestinationRequest): string;
//# sourceMappingURL=deleteotelforwardingdestination.d.ts.map