import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskSpan } from "./riskspan.js";
export type RiskResult = {
  /**
   * ID of the durable tool call block recorded for this finding's message, when one exists. Links to the block page at /blocks/:id.
   */
  blockId?: string | undefined;
  /**
   * The chat session containing the message.
   */
  chatId?: string | undefined;
  /**
   * The chat message that was scanned.
   */
  chatMessageId: string;
  /**
   * Title of the chat session.
   */
  chatTitle?: string | undefined;
  /**
   * Confidence score for this finding.
   */
  confidence?: number | undefined;
  /**
   * When this result was created.
   */
  createdAt: Date;
  /**
   * Human-readable description of the finding.
   */
  description?: string | undefined;
  /**
   * End byte position within the message content.
   */
  endPos?: number | undefined;
  /**
   * The result ID.
   */
  id: string;
  /**
   * The matched secret or sensitive data. Null when the caller isn't authorized to see raw match content for this result's chat (see match_redacted).
   */
  match?: string | undefined;
  /**
   * Opaque fingerprint of match, in the same `<redacted len=N sha=XXXXXXXX>` form as RiskResultRedacted.match_redacted. Populated whenever match is null so callers without raw access still get a stable, non-reversible correlation token. For shadow_mcp findings this is the original match value passed through verbatim (a non-sensitive server URL or command identifier).
   */
  matchRedacted?: string | undefined;
  /**
   * The risk policy ID.
   */
  policyId: string;
  /**
   * Policy version when this result was produced.
   */
  policyVersion: number;
  /**
   * The matched rule identifier.
   */
  ruleId?: string | undefined;
  /**
   * Detection source (e.g. gitleaks).
   */
  source: string;
  /**
   * All matched spans attributed to this finding. A finding may carry several correlated spans (e.g. a custom rule matching a tool's function name and its arguments on the same call). The top-level match/start_pos/end_pos mirror the primary (first) span. Null alongside match when the result is redacted.
   */
  spans?: Array<RiskSpan> | undefined;
  /**
   * Start byte position within the message content.
   */
  startPos?: number | undefined;
  /**
   * Tags from the detection rule.
   */
  tags?: Array<string> | undefined;
  /**
   * The user who owns the chat session.
   */
  userId?: string | undefined;
};
/** @internal */
export declare const RiskResult$inboundSchema: z.ZodMiniType<
  RiskResult,
  unknown
>;
export declare function riskResultFromJSON(
  jsonString: string,
): SafeParseResult<RiskResult, SDKValidationError>;
//# sourceMappingURL=riskresult.d.ts.map
