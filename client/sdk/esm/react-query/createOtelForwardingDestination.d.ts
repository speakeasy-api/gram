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
export type CreateOtelForwardingDestinationMutationVariables = {
  request: operations.CreateOtelForwardingDestinationRequest;
  security?: operations.CreateOtelForwardingDestinationSecurity | undefined;
  options?: RequestOptions;
};
export type CreateOtelForwardingDestinationMutationData =
  components.OtelForwardingDestination;
export type CreateOtelForwardingDestinationMutationError =
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
 * createDestination otelForwarding
 *
 * @remarks
 * Create a new OTEL forwarding destination. Name must be unique within the org (or org + project for project-scoped destinations).
 */
export declare function useCreateOtelForwardingDestinationMutation(
  options?: MutationHookOptions<
    CreateOtelForwardingDestinationMutationData,
    CreateOtelForwardingDestinationMutationError,
    CreateOtelForwardingDestinationMutationVariables
  >,
): UseMutationResult<
  CreateOtelForwardingDestinationMutationData,
  CreateOtelForwardingDestinationMutationError,
  CreateOtelForwardingDestinationMutationVariables
>;
export declare function mutationKeyCreateOtelForwardingDestination(): MutationKey;
export declare function buildCreateOtelForwardingDestinationMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateOtelForwardingDestinationMutationVariables,
  ) => Promise<CreateOtelForwardingDestinationMutationData>;
};
//# sourceMappingURL=createOtelForwardingDestination.d.ts.map
