import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type RiskApproveShadowMCPMutationVariables = {
  request: operations.ApproveShadowMCPRequest;
  security?: operations.ApproveShadowMCPSecurity | undefined;
  options?: RequestOptions;
};
export type RiskApproveShadowMCPMutationData = components.ShadowMCPApproval;
export type RiskApproveShadowMCPMutationError =
  | errors.ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * approveShadowMCP risk
 *
 * @remarks
 * Approve a shadow-MCP server so the named policy stops blocking calls to it. `match` is the same opaque server identifier surfaced in `RiskResult.match` — typically a server URL, stdio command, or `mcp__<server>__` prefix.
 */
export declare function useRiskApproveShadowMCPMutation(
  options?: MutationHookOptions<
    RiskApproveShadowMCPMutationData,
    RiskApproveShadowMCPMutationError,
    RiskApproveShadowMCPMutationVariables
  >,
): UseMutationResult<
  RiskApproveShadowMCPMutationData,
  RiskApproveShadowMCPMutationError,
  RiskApproveShadowMCPMutationVariables
>;
export declare function mutationKeyRiskApproveShadowMCP(): MutationKey;
export declare function buildRiskApproveShadowMCPMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RiskApproveShadowMCPMutationVariables,
  ) => Promise<RiskApproveShadowMCPMutationData>;
};
//# sourceMappingURL=riskApproveShadowMCP.d.ts.map
