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
  UpdatePackageRequest,
  UpdatePackageResponse,
  UpdatePackageSecurity,
} from "../models/operations/updatepackage.js";
import { MutationHookOptions } from "./_types.js";
export type UpdatePackageMutationVariables = {
  request: UpdatePackageRequest;
  security?: UpdatePackageSecurity | undefined;
  options?: RequestOptions;
};
export type UpdatePackageMutationData = UpdatePackageResponse;
export type UpdatePackageMutationError =
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
 * updatePackage packages
 *
 * @remarks
 * Update package details.
 */
export declare function useUpdatePackageMutation(
  options?: MutationHookOptions<
    UpdatePackageMutationData,
    UpdatePackageMutationError,
    UpdatePackageMutationVariables
  >,
): UseMutationResult<
  UpdatePackageMutationData,
  UpdatePackageMutationError,
  UpdatePackageMutationVariables
>;
export declare function mutationKeyUpdatePackage(): MutationKey;
export declare function buildUpdatePackageMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdatePackageMutationVariables,
  ) => Promise<UpdatePackageMutationData>;
};
//# sourceMappingURL=updatePackage.d.ts.map
