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
  UpdateTriggerInstanceRequest,
  UpdateTriggerInstanceSecurity,
} from "../models/operations/updatetriggerinstance.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateTriggerMutationVariables = {
  request: UpdateTriggerInstanceRequest;
  security?: UpdateTriggerInstanceSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateTriggerMutationData = TriggerInstance;
export type UpdateTriggerMutationError =
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
 * updateTriggerInstance triggers
 *
 * @remarks
 * Update a trigger instance.
 */
export declare function useUpdateTriggerMutation(
  options?: MutationHookOptions<
    UpdateTriggerMutationData,
    UpdateTriggerMutationError,
    UpdateTriggerMutationVariables
  >,
): UseMutationResult<
  UpdateTriggerMutationData,
  UpdateTriggerMutationError,
  UpdateTriggerMutationVariables
>;
export declare function mutationKeyUpdateTrigger(): MutationKey;
export declare function buildUpdateTriggerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateTriggerMutationVariables,
  ) => Promise<UpdateTriggerMutationData>;
};
//# sourceMappingURL=updateTrigger.d.ts.map
