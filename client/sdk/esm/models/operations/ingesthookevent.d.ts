import * as z from "zod/v4-mini";
import { IngestRequestBody, IngestRequestBody$Outbound } from "../components/ingestrequestbody.js";
export type IngestHookEventSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    projectSlugHeaderGramProject?: string | undefined;
};
export type IngestHookEventRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    /**
     * Optional per-invocation token reused across retries so the server stores a redelivered event exactly once.
     */
    idempotencyKey?: string | undefined;
    ingestRequestBody: IngestRequestBody;
};
/** @internal */
export type IngestHookEventSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "project_slug_header_Gram-Project"?: string | undefined;
};
/** @internal */
export declare const IngestHookEventSecurity$outboundSchema: z.ZodMiniType<IngestHookEventSecurity$Outbound, IngestHookEventSecurity>;
export declare function ingestHookEventSecurityToJSON(ingestHookEventSecurity: IngestHookEventSecurity): string;
/** @internal */
export type IngestHookEventRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    "Idempotency-Key"?: string | undefined;
    IngestRequestBody: IngestRequestBody$Outbound;
};
/** @internal */
export declare const IngestHookEventRequest$outboundSchema: z.ZodMiniType<IngestHookEventRequest$Outbound, IngestHookEventRequest>;
export declare function ingestHookEventRequestToJSON(ingestHookEventRequest: IngestHookEventRequest): string;
//# sourceMappingURL=ingesthookevent.d.ts.map