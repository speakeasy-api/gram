import * as z from "zod/v4-mini";
import { UpsertConfigRequestBody2, UpsertConfigRequestBody2$Outbound } from "../components/upsertconfigrequestbody2.js";
export type UpsertOtelForwardingConfigSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type UpsertOtelForwardingConfigRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    upsertConfigRequestBody2: UpsertConfigRequestBody2;
};
/** @internal */
export type UpsertOtelForwardingConfigSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpsertOtelForwardingConfigSecurity$outboundSchema: z.ZodMiniType<UpsertOtelForwardingConfigSecurity$Outbound, UpsertOtelForwardingConfigSecurity>;
export declare function upsertOtelForwardingConfigSecurityToJSON(upsertOtelForwardingConfigSecurity: UpsertOtelForwardingConfigSecurity): string;
/** @internal */
export type UpsertOtelForwardingConfigRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    UpsertConfigRequestBody2: UpsertConfigRequestBody2$Outbound;
};
/** @internal */
export declare const UpsertOtelForwardingConfigRequest$outboundSchema: z.ZodMiniType<UpsertOtelForwardingConfigRequest$Outbound, UpsertOtelForwardingConfigRequest>;
export declare function upsertOtelForwardingConfigRequestToJSON(upsertOtelForwardingConfigRequest: UpsertOtelForwardingConfigRequest): string;
//# sourceMappingURL=upsertotelforwardingconfig.d.ts.map