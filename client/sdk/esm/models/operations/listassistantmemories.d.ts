import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListAssistantMemoriesResult } from "../components/listassistantmemoriesresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListAssistantMemoriesSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ListAssistantMemoriesRequest = {
    /**
     * The assistant ID.
     */
    assistantId: string;
    /**
     * Optional tags to filter memories by.
     */
    tags?: Array<string> | undefined;
    /**
     * Whether to include soft-deleted memories.
     */
    includeDeleted?: boolean | undefined;
    /**
     * The cursor to fetch results from.
     */
    cursor?: string | undefined;
    /**
     * The number of memories to return per page.
     */
    limit?: number | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
export type ListAssistantMemoriesResponse = {
    result: ListAssistantMemoriesResult;
};
/** @internal */
export type ListAssistantMemoriesSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListAssistantMemoriesSecurity$outboundSchema: z.ZodMiniType<ListAssistantMemoriesSecurity$Outbound, ListAssistantMemoriesSecurity>;
export declare function listAssistantMemoriesSecurityToJSON(listAssistantMemoriesSecurity: ListAssistantMemoriesSecurity): string;
/** @internal */
export type ListAssistantMemoriesRequest$Outbound = {
    assistant_id: string;
    tags?: Array<string> | undefined;
    include_deleted: boolean;
    cursor?: string | undefined;
    limit: number;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListAssistantMemoriesRequest$outboundSchema: z.ZodMiniType<ListAssistantMemoriesRequest$Outbound, ListAssistantMemoriesRequest>;
export declare function listAssistantMemoriesRequestToJSON(listAssistantMemoriesRequest: ListAssistantMemoriesRequest): string;
/** @internal */
export declare const ListAssistantMemoriesResponse$inboundSchema: z.ZodMiniType<ListAssistantMemoriesResponse, unknown>;
export declare function listAssistantMemoriesResponseFromJSON(jsonString: string): SafeParseResult<ListAssistantMemoriesResponse, SDKValidationError>;
//# sourceMappingURL=listassistantmemories.d.ts.map