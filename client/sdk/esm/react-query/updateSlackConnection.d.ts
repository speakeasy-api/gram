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
export type UpdateSlackConnectionMutationVariables = {
  request: operations.UpdateSlackConnectionRequest;
  security?: operations.UpdateSlackConnectionSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateSlackConnectionMutationData =
  components.GetSlackConnectionResult;
export type UpdateSlackConnectionMutationError =
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
 * updateSlackConnection slack
 *
 * @remarks
 * update slack connection for an organization and project.
 */
export declare function useUpdateSlackConnectionMutation(
  options?: MutationHookOptions<
    UpdateSlackConnectionMutationData,
    UpdateSlackConnectionMutationError,
    UpdateSlackConnectionMutationVariables
  >,
): UseMutationResult<
  UpdateSlackConnectionMutationData,
  UpdateSlackConnectionMutationError,
  UpdateSlackConnectionMutationVariables
>;
export declare function mutationKeyUpdateSlackConnection(): MutationKey;
export declare function buildUpdateSlackConnectionMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateSlackConnectionMutationVariables,
  ) => Promise<UpdateSlackConnectionMutationData>;
};
//# sourceMappingURL=updateSlackConnection.d.ts.map
