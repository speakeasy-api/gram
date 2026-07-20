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
  DeleteAwsIamCredentialRequest,
  DeleteAwsIamCredentialSecurity,
} from "../models/operations/deleteawsiamcredential.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteAwsIamCredentialMutationVariables = {
  request: DeleteAwsIamCredentialRequest;
  security?: DeleteAwsIamCredentialSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteAwsIamCredentialMutationData = void;
export type DeleteAwsIamCredentialMutationError =
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
 * deleteAwsIamCredential externalCredentials
 *
 * @remarks
 * Soft-delete an AWS IAM external credential by ID. Requires org:admin.
 */
export declare function useDeleteAwsIamCredentialMutation(
  options?: MutationHookOptions<
    DeleteAwsIamCredentialMutationData,
    DeleteAwsIamCredentialMutationError,
    DeleteAwsIamCredentialMutationVariables
  >,
): UseMutationResult<
  DeleteAwsIamCredentialMutationData,
  DeleteAwsIamCredentialMutationError,
  DeleteAwsIamCredentialMutationVariables
>;
export declare function mutationKeyDeleteAwsIamCredential(): MutationKey;
export declare function buildDeleteAwsIamCredentialMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteAwsIamCredentialMutationVariables,
  ) => Promise<DeleteAwsIamCredentialMutationData>;
};
//# sourceMappingURL=deleteAwsIamCredential.d.ts.map
