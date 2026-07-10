import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type ListTelemetryLogsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListTelemetryLogsSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListTelemetryLogsSecurityOption3 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListTelemetryLogsSecurityOption4 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListTelemetryLogsSecurity = {
    option1?: ListTelemetryLogsSecurityOption1 | undefined;
    option2?: ListTelemetryLogsSecurityOption2 | undefined;
    option3?: ListTelemetryLogsSecurityOption3 | undefined;
    option4?: ListTelemetryLogsSecurityOption4 | undefined;
};
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
 * Sort order
 */
export declare const QueryParamSort: {
    readonly Asc: "asc";
    readonly Desc: "desc";
};
/**
 * Sort order
 */
export type QueryParamSort = ClosedEnum<typeof QueryParamSort>;
export type ListTelemetryLogsRequest = {
    /**
     * Start time in Unix nanoseconds
     */
    timeStart?: number | undefined;
    /**
     * End time in Unix nanoseconds
     */
    timeEnd?: number | undefined;
    /**
     * Gram URN filter
     */
    gramUrn?: string | undefined;
    /**
     * Trace ID filter (32 hex characters)
     */
    traceId?: string | undefined;
    /**
     * Deployment ID filter
     */
    deploymentId?: string | undefined;
    /**
     * Function ID filter
     */
    functionId?: string | undefined;
    /**
     * Severity level filter
     */
    severityText?: SeverityText | undefined;
    /**
     * HTTP status code filter
     */
    httpStatusCode?: number | undefined;
    /**
     * HTTP route filter
     */
    httpRoute?: string | undefined;
    /**
     * HTTP method filter
     */
    httpMethod?: HttpMethod | undefined;
    /**
     * Service name filter
     */
    serviceName?: string | undefined;
    /**
     * Cursor for pagination
     */
    cursor?: string | undefined;
    /**
     * Number of items to return (1-1000)
     */
    limit?: number | undefined;
    /**
     * Sort order
     */
    sort?: QueryParamSort | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type ListTelemetryLogsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListTelemetryLogsSecurityOption1$outboundSchema: z.ZodMiniType<ListTelemetryLogsSecurityOption1$Outbound, ListTelemetryLogsSecurityOption1>;
export declare function listTelemetryLogsSecurityOption1ToJSON(listTelemetryLogsSecurityOption1: ListTelemetryLogsSecurityOption1): string;
/** @internal */
export type ListTelemetryLogsSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListTelemetryLogsSecurityOption2$outboundSchema: z.ZodMiniType<ListTelemetryLogsSecurityOption2$Outbound, ListTelemetryLogsSecurityOption2>;
export declare function listTelemetryLogsSecurityOption2ToJSON(listTelemetryLogsSecurityOption2: ListTelemetryLogsSecurityOption2): string;
/** @internal */
export type ListTelemetryLogsSecurityOption3$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListTelemetryLogsSecurityOption3$outboundSchema: z.ZodMiniType<ListTelemetryLogsSecurityOption3$Outbound, ListTelemetryLogsSecurityOption3>;
export declare function listTelemetryLogsSecurityOption3ToJSON(listTelemetryLogsSecurityOption3: ListTelemetryLogsSecurityOption3): string;
/** @internal */
export type ListTelemetryLogsSecurityOption4$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListTelemetryLogsSecurityOption4$outboundSchema: z.ZodMiniType<ListTelemetryLogsSecurityOption4$Outbound, ListTelemetryLogsSecurityOption4>;
export declare function listTelemetryLogsSecurityOption4ToJSON(listTelemetryLogsSecurityOption4: ListTelemetryLogsSecurityOption4): string;
/** @internal */
export type ListTelemetryLogsSecurity$Outbound = {
    Option1?: ListTelemetryLogsSecurityOption1$Outbound | undefined;
    Option2?: ListTelemetryLogsSecurityOption2$Outbound | undefined;
    Option3?: ListTelemetryLogsSecurityOption3$Outbound | undefined;
    Option4?: ListTelemetryLogsSecurityOption4$Outbound | undefined;
};
/** @internal */
export declare const ListTelemetryLogsSecurity$outboundSchema: z.ZodMiniType<ListTelemetryLogsSecurity$Outbound, ListTelemetryLogsSecurity>;
export declare function listTelemetryLogsSecurityToJSON(listTelemetryLogsSecurity: ListTelemetryLogsSecurity): string;
/** @internal */
export declare const SeverityText$outboundSchema: z.ZodMiniEnum<typeof SeverityText>;
/** @internal */
export declare const HttpMethod$outboundSchema: z.ZodMiniEnum<typeof HttpMethod>;
/** @internal */
export declare const QueryParamSort$outboundSchema: z.ZodMiniEnum<typeof QueryParamSort>;
/** @internal */
export type ListTelemetryLogsRequest$Outbound = {
    time_start?: number | undefined;
    time_end?: number | undefined;
    gram_urn?: string | undefined;
    trace_id?: string | undefined;
    deployment_id?: string | undefined;
    function_id?: string | undefined;
    severity_text?: string | undefined;
    http_status_code?: number | undefined;
    http_route?: string | undefined;
    http_method?: string | undefined;
    service_name?: string | undefined;
    cursor?: string | undefined;
    limit: number;
    sort: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListTelemetryLogsRequest$outboundSchema: z.ZodMiniType<ListTelemetryLogsRequest$Outbound, ListTelemetryLogsRequest>;
export declare function listTelemetryLogsRequestToJSON(listTelemetryLogsRequest: ListTelemetryLogsRequest): string;
//# sourceMappingURL=listtelemetrylogs.d.ts.map