import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CodexHookResult } from "../models/components/codexhookresult.js";
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
  HooksNumberCodexRequest,
  HooksNumberCodexSecurity,
} from "../models/operations/hooksnumbercodex.js";
import { MutationHookOptions } from "./_types.js";
export type HooksHooksNumberCodexMutationVariables = {
  request: HooksNumberCodexRequest;
  security?: HooksNumberCodexSecurity | undefined;
  options?: RequestOptions;
};
export type HooksHooksNumberCodexMutationData = CodexHookResult;
export type HooksHooksNumberCodexMutationError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * codex hooks
 *
 * @remarks
 * Endpoint for Codex hook events. Handles SessionStart, PreToolUse, PermissionRequest, PostToolUse, UserPromptSubmit, and Stop.
 */
export declare function useHooksHooksNumberCodexMutation(
  options?: MutationHookOptions<
    HooksHooksNumberCodexMutationData,
    HooksHooksNumberCodexMutationError,
    HooksHooksNumberCodexMutationVariables
  >,
): UseMutationResult<
  HooksHooksNumberCodexMutationData,
  HooksHooksNumberCodexMutationError,
  HooksHooksNumberCodexMutationVariables
>;
export declare function mutationKeyHooksHooksNumberCodex(): MutationKey;
export declare function buildHooksHooksNumberCodexMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: HooksHooksNumberCodexMutationVariables,
  ) => Promise<HooksHooksNumberCodexMutationData>;
};
//# sourceMappingURL=hooksHooksNumberCodex.d.ts.map
