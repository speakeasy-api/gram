import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskResultsForAgentResult } from "../models/components/listriskresultsforagentresult.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  ListRiskResultsForAgentRequest,
  ListRiskResultsForAgentSecurity,
} from "../models/operations/listriskresultsforagent.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listRiskResultsForAgent risk
 *
 * @remarks
 * List risk analysis results with the `match` field redacted to an opaque length+sha256-prefix fingerprint. Matches the payload and pagination semantics of listRiskResults. Designed for AI assistant / MCP consumption so secret content (gitleaks captures, presidio entities, prompt-injection payloads) never reaches the model context. For shadow_mcp findings the `match` value — a non-sensitive server URL or command identifier — is passed through verbatim.
 */
export declare function riskResultsListForAgent(
  client: GramCore,
  request?: ListRiskResultsForAgentRequest | undefined,
  security?: ListRiskResultsForAgentSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListRiskResultsForAgentResult,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=riskResultsListForAgent.d.ts.map
