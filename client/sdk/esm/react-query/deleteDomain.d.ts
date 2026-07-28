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
  DeleteDomainRequest,
  DeleteDomainSecurity,
} from "../models/operations/deletedomain.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteDomainMutationVariables = {
  request?: DeleteDomainRequest | undefined;
  security?: DeleteDomainSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteDomainMutationData = void;
export type DeleteDomainMutationError =
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
 * deleteDomain domains
 *
 * @remarks
 * Delete a custom domain
 */
export declare function useDeleteDomainMutation(
  options?: MutationHookOptions<
    DeleteDomainMutationData,
    DeleteDomainMutationError,
    DeleteDomainMutationVariables
  >,
): UseMutationResult<
  DeleteDomainMutationData,
  DeleteDomainMutationError,
  DeleteDomainMutationVariables
>;
export declare function mutationKeyDeleteDomain(): MutationKey;
export declare function buildDeleteDomainMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteDomainMutationVariables,
  ) => Promise<DeleteDomainMutationData>;
};
//# sourceMappingURL=deleteDomain.d.ts.map
