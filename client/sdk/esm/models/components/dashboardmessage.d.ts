import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Message author.
 */
export declare const DashboardMessageRole: {
    readonly User: "user";
    readonly Assistant: "assistant";
};
/**
 * Message author.
 */
export type DashboardMessageRole = ClosedEnum<typeof DashboardMessageRole>;
export type DashboardMessage = {
    /**
     * Message content (Markdown).
     */
    content: string;
    /**
     * RFC3339 creation timestamp.
     */
    createdAt: Date;
    /**
     * Message id.
     */
    id: string;
    /**
     * Message author.
     */
    role: DashboardMessageRole;
    /**
     * Monotonic cursor; pass the latest value as after_seq to poll for newer messages.
     */
    seq: number;
};
/** @internal */
export declare const DashboardMessageRole$inboundSchema: z.ZodMiniEnum<typeof DashboardMessageRole>;
/** @internal */
export declare const DashboardMessage$inboundSchema: z.ZodMiniType<DashboardMessage, unknown>;
export declare function dashboardMessageFromJSON(jsonString: string): SafeParseResult<DashboardMessage, SDKValidationError>;
//# sourceMappingURL=dashboardmessage.d.ts.map