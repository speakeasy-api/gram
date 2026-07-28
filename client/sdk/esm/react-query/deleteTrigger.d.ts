import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  DeleteTriggerInstanceRequest,
  DeleteTriggerInstanceSecurity,
} from "../models/operations/deletetriggerinstance.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteTriggerMutationVariables = {
  request: DeleteTriggerInstanceRequest;
  security?: DeleteTriggerInstanceSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteTriggerMutationData = void;
export type DeleteTriggerMutationError =
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
 * deleteTriggerInstance triggers
 *
 * @remarks
 * Delete a trigger instance.
 */
export declare function useDeleteTriggerMutation(
  options?: MutationHookOptions<
    DeleteTriggerMutationData,
    DeleteTriggerMutationError,
    DeleteTriggerMutationVariables
  >,
): UseMutationResult<
  DeleteTriggerMutationData,
  DeleteTriggerMutationError,
  DeleteTriggerMutationVariables
>;
export declare function mutationKeyDeleteTrigger(): MutationKey;
export declare function buildDeleteTriggerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteTriggerMutationVariables,
  ) => Promise<DeleteTriggerMutationData>;
};
//# sourceMappingURL=deleteTrigger.d.ts.map
