import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A contiguous run of messages in a windowed view (`risk_only` via `risk_segments`, or `query` via `match_segments`), covering one or more matches plus their surrounding context. Messages for a segment are the entries of `Chat.messages` whose `seq` falls within `[first_seq, last_seq]`.
 */
export type RiskSegment = {
    /**
     * The `seq` of the first (oldest) message in this segment.
     */
    firstSeq: number;
    /**
     * Whether messages exist after this segment within the generation. Expand with an `after_seq` request using `last_seq`.
     */
    hasMoreAfter: boolean;
    /**
     * Whether messages exist before this segment within the generation. Expand with a `before_seq` request using `first_seq`.
     */
    hasMoreBefore: boolean;
    /**
     * The `seq` of the last (newest) message in this segment.
     */
    lastSeq: number;
};
/** @internal */
export declare const RiskSegment$inboundSchema: z.ZodMiniType<RiskSegment, unknown>;
export declare function riskSegmentFromJSON(jsonString: string): SafeParseResult<RiskSegment, SDKValidationError>;
//# sourceMappingURL=risksegment.d.ts.map