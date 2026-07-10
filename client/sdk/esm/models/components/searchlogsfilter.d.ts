import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * HTTP method filter
 */
export declare const HttpMethod: {
    readonly Get: "GET";
    readonly Post: "POST";
    readonly Put: "PUT";
    readonly Patch: "PATCH";
    readonly Delete: "DELETE";
    readonly Head: "HEAD";
    readonly Options: "OPTIONS";
};
/**
 * HTTP method filter
 */
export type HttpMethod = ClosedEnum<typeof HttpMethod>;
/**
 * Severity level filter
 */
export declare const SeverityText: {
    readonly Debug: "DEBUG";
    readonly Info: "INFO";
    readonly Warn: "WARN";
    readonly Error: "ERROR";
    readonly Fatal: "FATAL";
};
/**
 * Severity level filter
 */
export type SeverityText = ClosedEnum<typeof SeverityText>;
/**
 * Filter criteria for searching logs
 */
export type SearchLogsFilter = {
    /**
     * Deployment ID filter
     */
    deploymentId?: string | undefined;
    /**
     * Event source filter (e.g., 'hook', 'tool_call', 'chat_completion')
     */
    eventSource?: string | undefined;
    /**
     * External user ID filter
     */
    externalUserId?: string | undefined;
    /**
     * Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')
     */
    from?: Date | undefined;
    /**
     * Function ID filter
     */
    functionId?: string | undefined;
    /**
     * Chat ID filter
     */
    gramChatId?: string | undefined;
    /**
     * Gram URN filter (single URN, use gram_urns for multiple)
     */
    gramUrn?: string | undefined;
    /**
     * Gram URN filter (one or more URNs)
     */
    gramUrns?: Array<string> | undefined;
    /**
     * HTTP method filter
     */
    httpMethod?: HttpMethod | undefined;
    /**
     * HTTP route filter
     */
    httpRoute?: string | undefined;
    /**
     * HTTP status code filter
     */
    httpStatusCode?: number | undefined;
    /**
     * Service name filter
     */
    serviceName?: string | undefined;
    /**
     * Severity level filter
     */
    severityText?: SeverityText | undefined;
    /**
     * End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')
     */
    to?: Date | undefined;
    /**
     * Trace ID filter (32 hex characters)
     */
    traceId?: string | undefined;
    /**
     * User ID filter
     */
    userId?: string | undefined;
};
/** @internal */
export declare const HttpMethod$outboundSchema: z.ZodMiniEnum<typeof HttpMethod>;
/** @internal */
export declare const SeverityText$outboundSchema: z.ZodMiniEnum<typeof SeverityText>;
/** @internal */
export type SearchLogsFilter$Outbound = {
    deployment_id?: string | undefined;
    event_source?: string | undefined;
    external_user_id?: string | undefined;
    from?: string | undefined;
    function_id?: string | undefined;
    gram_chat_id?: string | undefined;
    gram_urn?: string | undefined;
    gram_urns?: Array<string> | undefined;
    http_method?: string | undefined;
    http_route?: string | undefined;
    http_status_code?: number | undefined;
    service_name?: string | undefined;
    severity_text?: string | undefined;
    to?: string | undefined;
    trace_id?: string | undefined;
    user_id?: string | undefined;
};
/** @internal */
export declare const SearchLogsFilter$outboundSchema: z.ZodMiniType<SearchLogsFilter$Outbound, SearchLogsFilter>;
export declare function searchLogsFilterToJSON(searchLogsFilter: SearchLogsFilter): string;
//# sourceMappingURL=searchlogsfilter.d.ts.map