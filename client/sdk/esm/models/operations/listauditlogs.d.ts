import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListAuditLogsResult } from "../components/listauditlogsresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListAuditLogsSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ListAuditLogsRequest = {
    /**
     * The cursor for paginating through audit logs.
     */
    cursor?: string | undefined;
    /**
     * Project slug to filter audit logs to a specific project.
     */
    projectSlug?: string | undefined;
    /**
     * Actor ID to filter audit logs to a specific actor.
     */
    actorId?: string | undefined;
    /**
     * Action to filter audit logs to a specific action.
     */
    action?: string | undefined;
    /**
     * Subject type to filter audit logs to a specific kind of subject. When omitted, assistant activity events are excluded; pass 'assistant' to list them.
     */
    subjectType?: string | undefined;
    /**
     * Subject ID to filter audit logs to a specific subject (e.g. a single assistant).
     */
    subjectId?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
export type ListAuditLogsResponse = {
    result: ListAuditLogsResult;
};
/** @internal */
export type ListAuditLogsSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListAuditLogsSecurity$outboundSchema: z.ZodMiniType<ListAuditLogsSecurity$Outbound, ListAuditLogsSecurity>;
export declare function listAuditLogsSecurityToJSON(listAuditLogsSecurity: ListAuditLogsSecurity): string;
/** @internal */
export type ListAuditLogsRequest$Outbound = {
    cursor?: string | undefined;
    project_slug?: string | undefined;
    actor_id?: string | undefined;
    action?: string | undefined;
    subject_type?: string | undefined;
    subject_id?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListAuditLogsRequest$outboundSchema: z.ZodMiniType<ListAuditLogsRequest$Outbound, ListAuditLogsRequest>;
export declare function listAuditLogsRequestToJSON(listAuditLogsRequest: ListAuditLogsRequest): string;
/** @internal */
export declare const ListAuditLogsResponse$inboundSchema: z.ZodMiniType<ListAuditLogsResponse, unknown>;
export declare function listAuditLogsResponseFromJSON(jsonString: string): SafeParseResult<ListAuditLogsResponse, SDKValidationError>;
//# sourceMappingURL=listauditlogs.d.ts.map