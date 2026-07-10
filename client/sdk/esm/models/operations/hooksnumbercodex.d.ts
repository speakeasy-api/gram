import * as z from "zod/v4-mini";
import { CodexHookPayload, CodexHookPayload$Outbound } from "../components/codexhookpayload.js";
export type HooksNumberCodexSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    projectSlugHeaderGramProject?: string | undefined;
};
export type HooksNumberCodexRequest = {
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
    codexHookPayload: CodexHookPayload;
};
/** @internal */
export type HooksNumberCodexSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "project_slug_header_Gram-Project"?: string | undefined;
};
/** @internal */
export declare const HooksNumberCodexSecurity$outboundSchema: z.ZodMiniType<HooksNumberCodexSecurity$Outbound, HooksNumberCodexSecurity>;
export declare function hooksNumberCodexSecurityToJSON(hooksNumberCodexSecurity: HooksNumberCodexSecurity): string;
/** @internal */
export type HooksNumberCodexRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    "X-Gram-Hook-Hostname"?: string | undefined;
    "Idempotency-Key"?: string | undefined;
    CodexHookPayload: CodexHookPayload$Outbound;
};
/** @internal */
export declare const HooksNumberCodexRequest$outboundSchema: z.ZodMiniType<HooksNumberCodexRequest$Outbound, HooksNumberCodexRequest>;
export declare function hooksNumberCodexRequestToJSON(hooksNumberCodexRequest: HooksNumberCodexRequest): string;
//# sourceMappingURL=hooksnumbercodex.d.ts.map