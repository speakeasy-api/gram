import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListRiskResultsByChatResult } from "../models/components/listriskresultsbychatresult.js";
import { ListRiskResultsForAgentResult } from "../models/components/listriskresultsforagentresult.js";
import { ListRiskResultsResult } from "../models/components/listriskresultsresult.js";
import { RiskUnmaskResultResult } from "../models/components/riskunmaskresultresult.js";
import {
  ListRiskResultsRequest,
  ListRiskResultsSecurity,
} from "../models/operations/listriskresults.js";
import {
  ListRiskResultsByChatRequest,
  ListRiskResultsByChatSecurity,
} from "../models/operations/listriskresultsbychat.js";
import {
  ListRiskResultsForAgentRequest,
  ListRiskResultsForAgentSecurity,
} from "../models/operations/listriskresultsforagent.js";
import {
  UnmaskRiskResultRequest,
  UnmaskRiskResultSecurity,
} from "../models/operations/unmaskriskresult.js";
export declare class Results extends ClientSDK {
  /**
   * listRiskResults risk
   *
   * @remarks
   * List risk analysis results for the current project.
   */
  list(
    request?: ListRiskResultsRequest | undefined,
    security?: ListRiskResultsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListRiskResultsResult>;
  /**
   * listRiskResultsByChat risk
   *
   * @remarks
   * List risk results grouped by chat session for the current project.
   */
  byChat(
    request?: ListRiskResultsByChatRequest | undefined,
    security?: ListRiskResultsByChatSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListRiskResultsByChatResult>;
  /**
   * listRiskResultsForAgent risk
   *
   * @remarks
   * List risk analysis results with the `match` field redacted to an opaque length+sha256-prefix fingerprint. Matches the payload and pagination semantics of listRiskResults. Designed for AI assistant / MCP consumption so secret content (gitleaks captures, presidio entities, prompt-injection payloads) never reaches the model context. For shadow_mcp findings the `match` value — a non-sensitive server URL or command identifier — is passed through verbatim.
   */
  listForAgent(
    request?: ListRiskResultsForAgentRequest | undefined,
    security?: ListRiskResultsForAgentSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListRiskResultsForAgentResult>;
  /**
   * unmaskRiskResult risk
   *
   * @remarks
   * Return the plaintext match for a single risk result, on demand. Gated on the chat:read scope for the result's chat (not org:admin) — reveal is a discrete, audited access event distinct from listing redacted results.
   */
  unmask(
    request: UnmaskRiskResultRequest,
    security?: UnmaskRiskResultSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskUnmaskResultResult>;
}
//# sourceMappingURL=results.d.ts.map
