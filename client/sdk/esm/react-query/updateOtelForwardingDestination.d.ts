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
export type UpdateOtelForwardingDestinationMutationVariables = {
  request: operations.UpdateOtelForwardingDestinationRequest;
  security?: operations.UpdateOtelForwardingDestinationSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateOtelForwardingDestinationMutationData =
  components.OtelForwardingDestination;
export type UpdateOtelForwardingDestinationMutationError =
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
 * updateDestination otelForwarding
 *
 * @remarks
 * Replace every mutable field on a forwarding destination. Headers are fully replaced — pass the full set on each call.
 */
export declare function useUpdateOtelForwardingDestinationMutation(
  options?: MutationHookOptions<
    UpdateOtelForwardingDestinationMutationData,
    UpdateOtelForwardingDestinationMutationError,
    UpdateOtelForwardingDestinationMutationVariables
  >,
): UseMutationResult<
  UpdateOtelForwardingDestinationMutationData,
  UpdateOtelForwardingDestinationMutationError,
  UpdateOtelForwardingDestinationMutationVariables
>;
export declare function mutationKeyUpdateOtelForwardingDestination(): MutationKey;
export declare function buildUpdateOtelForwardingDestinationMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateOtelForwardingDestinationMutationVariables,
  ) => Promise<UpdateOtelForwardingDestinationMutationData>;
};
//# sourceMappingURL=updateOtelForwardingDestination.d.ts.map
