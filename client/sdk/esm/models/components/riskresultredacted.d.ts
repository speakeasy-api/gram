import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskSpanRedacted } from "./riskspanredacted.js";
export type RiskResultRedacted = {
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
   * The result ID.
   */
  id: string;
  /**
   * Opaque fingerprint of the original match in the form `<redacted len=N sha=XXXXXXXX>` where N is the byte length of the original match and XXXXXXXX is the first 8 hex characters of sha256(match). For shadow_mcp findings the original match value (a non-sensitive server URL or command identifier) is passed through verbatim.
   */
  matchRedacted: string;
  /**
   * The risk policy ID.
   */
  policyId: string;
  /**
   * Policy version when this result was produced.
   */
  policyVersion: number;
  /**
   * Whether the original finding carried byte-position information within the source message. Exact positions are intentionally not exposed to avoid reconstruction attacks.
   */
  positionKnown: boolean;
  /**
   * The matched rule identifier.
   */
  ruleId?: string | undefined;
  /**
   * Detection source (e.g. gitleaks, presidio, shadow_mcp).
   */
  source: string;
  /**
   * All matched spans attributed to this finding, each with its match replaced by an opaque fingerprint.
   */
  spansRedacted?: Array<RiskSpanRedacted> | undefined;
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
export declare const RiskResultRedacted$inboundSchema: z.ZodMiniType<
  RiskResultRedacted,
  unknown
>;
export declare function riskResultRedactedFromJSON(
  jsonString: string,
): SafeParseResult<RiskResultRedacted, SDKValidationError>;
//# sourceMappingURL=riskresultredacted.d.ts.map
