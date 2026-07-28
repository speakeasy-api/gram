import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreatePackageResult } from "../models/components/createpackageresult.js";
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
  CreatePackageRequest,
  CreatePackageSecurity,
} from "../models/operations/createpackage.js";
import { MutationHookOptions } from "./_types.js";
export type CreatePackageMutationVariables = {
  request: CreatePackageRequest;
  security?: CreatePackageSecurity | undefined;
  options?: RequestOptions;
};
export type CreatePackageMutationData = CreatePackageResult;
export type CreatePackageMutationError =
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
 * createPackage packages
 *
 * @remarks
 * Create a new package for a project.
 */
export declare function useCreatePackageMutation(
  options?: MutationHookOptions<
    CreatePackageMutationData,
    CreatePackageMutationError,
    CreatePackageMutationVariables
  >,
): UseMutationResult<
  CreatePackageMutationData,
  CreatePackageMutationError,
  CreatePackageMutationVariables
>;
export declare function mutationKeyCreatePackage(): MutationKey;
export declare function buildCreatePackageMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreatePackageMutationVariables,
  ) => Promise<CreatePackageMutationData>;
};
//# sourceMappingURL=createPackage.d.ts.map
