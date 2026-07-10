import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CursorHookResult } from "../models/components/cursorhookresult.js";
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
  HooksNumberCursorRequest,
  HooksNumberCursorSecurity,
} from "../models/operations/hooksnumbercursor.js";
import { MutationHookOptions } from "./_types.js";
export type HooksHooksNumberCursorMutationVariables = {
  request: HooksNumberCursorRequest;
  security?: HooksNumberCursorSecurity | undefined;
  options?: RequestOptions;
};
export type HooksHooksNumberCursorMutationData = CursorHookResult;
export type HooksHooksNumberCursorMutationError =
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
 * cursor hooks
 *
 * @remarks
 * Endpoint for Cursor hook events. Handles beforeSubmitPrompt, stop, afterAgentResponse, afterAgentThought, preToolUse, postToolUse, postToolUseFailure, beforeMCPExecution, and afterMCPExecution.
 */
export declare function useHooksHooksNumberCursorMutation(
  options?: MutationHookOptions<
    HooksHooksNumberCursorMutationData,
    HooksHooksNumberCursorMutationError,
    HooksHooksNumberCursorMutationVariables
  >,
): UseMutationResult<
  HooksHooksNumberCursorMutationData,
  HooksHooksNumberCursorMutationError,
  HooksHooksNumberCursorMutationVariables
>;
export declare function mutationKeyHooksHooksNumberCursor(): MutationKey;
export declare function buildHooksHooksNumberCursorMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: HooksHooksNumberCursorMutationVariables,
  ) => Promise<HooksHooksNumberCursorMutationData>;
};
//# sourceMappingURL=hooksHooksNumberCursor.d.ts.map
