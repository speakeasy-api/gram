import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type AssistantMemory = {
    /**
     * The assistant ID owning the memory.
     */
    assistantId: string;
    /**
     * The memory content.
     */
    content: string;
    /**
     * Creation timestamp.
     */
    createdAt: Date;
    /**
     * Timestamp at which the memory was soft-deleted.
     */
    deletedAt?: Date | undefined;
    /**
     * The assistant memory ID.
     */
    id: string;
    /**
     * Timestamp of the most recent access.
     */
    lastAccess: Date;
    /**
     * Timestamp at which the memory was superseded by another memory.
     */
    supersededAt?: Date | undefined;
    /**
     * The ID of the memory this one supersedes, if any.
     */
    supersedesId?: string | undefined;
    /**
     * Tags associated with the memory.
     */
    tags: Array<string>;
    /**
     * Last update timestamp.
     */
    updatedAt: Date;
    /**
     * Timestamp at which the memory becomes valid.
     */
    validAt: Date;
};
/** @internal */
export declare const AssistantMemory$inboundSchema: z.ZodMiniType<AssistantMemory, unknown>;
export declare function assistantMemoryFromJSON(jsonString: string): SafeParseResult<AssistantMemory, SDKValidationError>;
//# sourceMappingURL=assistantmemory.d.ts.map