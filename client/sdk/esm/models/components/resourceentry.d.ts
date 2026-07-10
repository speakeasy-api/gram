import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export declare const ResourceEntryType: {
    readonly Function: "function";
};
export type ResourceEntryType = ClosedEnum<typeof ResourceEntryType>;
export type ResourceEntry = {
    /**
     * The ID of the resource
     */
    id: string;
    /**
     * The name of the resource
     */
    name: string;
    /**
     * The URN of the resource
     */
    resourceUrn: string;
    type: ResourceEntryType;
    /**
     * The uri of the resource
     */
    uri: string;
};
/** @internal */
export declare const ResourceEntryType$inboundSchema: z.ZodMiniEnum<typeof ResourceEntryType>;
/** @internal */
export declare const ResourceEntry$inboundSchema: z.ZodMiniType<ResourceEntry, unknown>;
export declare function resourceEntryFromJSON(jsonString: string): SafeParseResult<ResourceEntry, SDKValidationError>;
//# sourceMappingURL=resourceentry.d.ts.map