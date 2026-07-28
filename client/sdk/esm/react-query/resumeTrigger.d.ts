import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TriggerInstance } from "../models/components/triggerinstance.js";
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
  ResumeTriggerInstanceRequest,
  ResumeTriggerInstanceSecurity,
} from "../models/operations/resumetriggerinstance.js";
import { MutationHookOptions } from "./_types.js";
export type ResumeTriggerMutationVariables = {
  request: ResumeTriggerInstanceRequest;
  security?: ResumeTriggerInstanceSecurity | undefined;
  options?: RequestOptions;
};
export type ResumeTriggerMutationData = TriggerInstance;
export type ResumeTriggerMutationError =
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
 * resumeTriggerInstance triggers
 *
 * @remarks
 * Resume a trigger instance.
 */
export declare function useResumeTriggerMutation(
  options?: MutationHookOptions<
    ResumeTriggerMutationData,
    ResumeTriggerMutationError,
    ResumeTriggerMutationVariables
  >,
): UseMutationResult<
  ResumeTriggerMutationData,
  ResumeTriggerMutationError,
  ResumeTriggerMutationVariables
>;
export declare function mutationKeyResumeTrigger(): MutationKey;
export declare function buildResumeTriggerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: ResumeTriggerMutationVariables,
  ) => Promise<ResumeTriggerMutationData>;
};
//# sourceMappingURL=resumeTrigger.d.ts.map
