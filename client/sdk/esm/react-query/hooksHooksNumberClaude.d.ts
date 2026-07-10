import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ClaudeHookResult } from "../models/components/claudehookresult.js";
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
import { HooksNumberClaudeRequest } from "../models/operations/hooksnumberclaude.js";
import { MutationHookOptions } from "./_types.js";
export type HooksHooksNumberClaudeMutationVariables = {
  request: HooksNumberClaudeRequest;
  options?: RequestOptions;
};
export type HooksHooksNumberClaudeMutationData = ClaudeHookResult;
export type HooksHooksNumberClaudeMutationError =
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
 * claude hooks
 *
 * @remarks
 * Unified endpoint for all Claude Code hook events. Handles SessionStart, PreToolUse, PostToolUse, and PostToolUseFailure.
 */
export declare function useHooksHooksNumberClaudeMutation(
  options?: MutationHookOptions<
    HooksHooksNumberClaudeMutationData,
    HooksHooksNumberClaudeMutationError,
    HooksHooksNumberClaudeMutationVariables
  >,
): UseMutationResult<
  HooksHooksNumberClaudeMutationData,
  HooksHooksNumberClaudeMutationError,
  HooksHooksNumberClaudeMutationVariables
>;
export declare function mutationKeyHooksHooksNumberClaude(): MutationKey;
export declare function buildHooksHooksNumberClaudeMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: HooksHooksNumberClaudeMutationVariables,
  ) => Promise<HooksHooksNumberClaudeMutationData>;
};
//# sourceMappingURL=hooksHooksNumberClaude.d.ts.map
