import * as z from "zod/v4-mini";
import { CursorHookPayload, CursorHookPayload$Outbound } from "../components/cursorhookpayload.js";
export type HooksNumberCursorSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    projectSlugHeaderGramProject?: string | undefined;
};
export type HooksNumberCursorRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    /**
     * Optional endpoint hostname supplied by the Gram hook plugin.
     */
    xGramHookHostname?: string | undefined;
    /**
     * Optional per-invocation token reused across retries so the server stores a redelivered event exactly once.
     */
    idempotencyKey?: string | undefined;
    cursorHookPayload: CursorHookPayload;
};
/** @internal */
export type HooksNumberCursorSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "project_slug_header_Gram-Project"?: string | undefined;
};
/** @internal */
export declare const HooksNumberCursorSecurity$outboundSchema: z.ZodMiniType<HooksNumberCursorSecurity$Outbound, HooksNumberCursorSecurity>;
export declare function hooksNumberCursorSecurityToJSON(hooksNumberCursorSecurity: HooksNumberCursorSecurity): string;
/** @internal */
export type HooksNumberCursorRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    "X-Gram-Hook-Hostname"?: string | undefined;
    "Idempotency-Key"?: string | undefined;
    CursorHookPayload: CursorHookPayload$Outbound;
};
/** @internal */
export declare const HooksNumberCursorRequest$outboundSchema: z.ZodMiniType<HooksNumberCursorRequest$Outbound, HooksNumberCursorRequest>;
export declare function hooksNumberCursorRequestToJSON(hooksNumberCursorRequest: HooksNumberCursorRequest): string;
//# sourceMappingURL=hooksnumbercursor.d.ts.map