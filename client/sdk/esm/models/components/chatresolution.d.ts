import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Resolution information for a chat
 */
export type ChatResolution = {
    /**
     * When resolution was created
     */
    createdAt: Date;
    /**
     * Resolution ID
     */
    id: string;
    /**
     * Message IDs associated with this resolution
     */
    messageIds: Array<string>;
    /**
     * Resolution status
     */
    resolution: string;
    /**
     * Notes about the resolution
     */
    resolutionNotes: string;
    /**
     * Score 0-100
     */
    score: number;
    /**
     * User's intended goal
     */
    userGoal: string;
};
/** @internal */
export declare const ChatResolution$inboundSchema: z.ZodMiniType<ChatResolution, unknown>;
export declare function chatResolutionFromJSON(jsonString: string): SafeParseResult<ChatResolution, SDKValidationError>;
//# sourceMappingURL=chatresolution.d.ts.map