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
  CreateTriggerInstanceRequest,
  CreateTriggerInstanceSecurity,
} from "../models/operations/createtriggerinstance.js";
import { MutationHookOptions } from "./_types.js";
export type CreateTriggerMutationVariables = {
  request: CreateTriggerInstanceRequest;
  security?: CreateTriggerInstanceSecurity | undefined;
  options?: RequestOptions;
};
export type CreateTriggerMutationData = TriggerInstance;
export type CreateTriggerMutationError =
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
 * createTriggerInstance triggers
 *
 * @remarks
 * Create a trigger instance.
 */
export declare function useCreateTriggerMutation(
  options?: MutationHookOptions<
    CreateTriggerMutationData,
    CreateTriggerMutationError,
    CreateTriggerMutationVariables
  >,
): UseMutationResult<
  CreateTriggerMutationData,
  CreateTriggerMutationError,
  CreateTriggerMutationVariables
>;
export declare function mutationKeyCreateTrigger(): MutationKey;
export declare function buildCreateTriggerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateTriggerMutationVariables,
  ) => Promise<CreateTriggerMutationData>;
};
//# sourceMappingURL=createTrigger.d.ts.map
