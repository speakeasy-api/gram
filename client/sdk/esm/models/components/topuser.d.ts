import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Type of user ID
 */
export declare const UserType: {
    readonly Internal: "internal";
    readonly External: "external";
};
/**
 * Type of user ID
 */
export type UserType = ClosedEnum<typeof UserType>;
/**
 * Top user by activity
 */
export type TopUser = {
    /**
     * Number of messages (session mode) or tool calls (tool_call mode)
     */
    activityCount: number;
    /**
     * User ID (internal or external depending on availability)
     */
    userId: string;
    /**
     * Type of user ID
     */
    userType: UserType;
};
/** @internal */
export declare const UserType$inboundSchema: z.ZodMiniEnum<typeof UserType>;
/** @internal */
export declare const TopUser$inboundSchema: z.ZodMiniType<TopUser, unknown>;
export declare function topUserFromJSON(jsonString: string): SafeParseResult<TopUser, SDKValidationError>;
//# sourceMappingURL=topuser.d.ts.map