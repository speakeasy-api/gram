import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type UpdateOtelForwardingDestinationSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type UpdateOtelForwardingDestinationRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    updateDestinationRequestBody: components.UpdateDestinationRequestBody;
};
/** @internal */
export type UpdateOtelForwardingDestinationSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateOtelForwardingDestinationSecurity$outboundSchema: z.ZodMiniType<UpdateOtelForwardingDestinationSecurity$Outbound, UpdateOtelForwardingDestinationSecurity>;
export declare function updateOtelForwardingDestinationSecurityToJSON(updateOtelForwardingDestinationSecurity: UpdateOtelForwardingDestinationSecurity): string;
/** @internal */
export type UpdateOtelForwardingDestinationRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    UpdateDestinationRequestBody: components.UpdateDestinationRequestBody$Outbound;
};
/** @internal */
export declare const UpdateOtelForwardingDestinationRequest$outboundSchema: z.ZodMiniType<UpdateOtelForwardingDestinationRequest$Outbound, UpdateOtelForwardingDestinationRequest>;
export declare function updateOtelForwardingDestinationRequestToJSON(updateOtelForwardingDestinationRequest: UpdateOtelForwardingDestinationRequest): string;
//# sourceMappingURL=updateotelforwardingdestination.d.ts.map