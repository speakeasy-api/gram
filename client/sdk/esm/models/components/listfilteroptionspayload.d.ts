import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Type of filter to list options for
 */
export declare const FilterType: {
    readonly ApiKey: "api_key";
    readonly User: "user";
    readonly InternalUser: "internal_user";
    readonly Agent: "agent";
};
/**
 * Type of filter to list options for
 */
export type FilterType = ClosedEnum<typeof FilterType>;
/**
 * Payload for listing filter options
 */
export type ListFilterOptionsPayload = {
    /**
     * Optional event source filter for the option list
     */
    eventSource?: string | undefined;
    /**
     * Type of filter to list options for
     */
    filterType: FilterType;
    /**
     * Start time in ISO 8601 format
     */
    from: Date;
    /**
     * End time in ISO 8601 format
     */
    to: Date;
};
/** @internal */
export declare const FilterType$outboundSchema: z.ZodMiniEnum<typeof FilterType>;
/** @internal */
export type ListFilterOptionsPayload$Outbound = {
    event_source?: string | undefined;
    filter_type: string;
    from: string;
    to: string;
};
/** @internal */
export declare const ListFilterOptionsPayload$outboundSchema: z.ZodMiniType<ListFilterOptionsPayload$Outbound, ListFilterOptionsPayload>;
export declare function listFilterOptionsPayloadToJSON(listFilterOptionsPayload: ListFilterOptionsPayload): string;
//# sourceMappingURL=listfilteroptionspayload.d.ts.map