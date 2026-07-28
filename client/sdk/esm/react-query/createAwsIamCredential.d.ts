import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AwsIamCredential } from "../models/components/awsiamcredential.js";
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
  CreateAwsIamCredentialRequest,
  CreateAwsIamCredentialSecurity,
} from "../models/operations/createawsiamcredential.js";
import { MutationHookOptions } from "./_types.js";
export type CreateAwsIamCredentialMutationVariables = {
  request: CreateAwsIamCredentialRequest;
  security?: CreateAwsIamCredentialSecurity | undefined;
  options?: RequestOptions;
};
export type CreateAwsIamCredentialMutationData = AwsIamCredential;
export type CreateAwsIamCredentialMutationError =
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
 * createAwsIamCredential externalCredentials
 *
 * @remarks
 * Create an AWS IAM external credential. Requires org:admin.
 */
export declare function useCreateAwsIamCredentialMutation(
  options?: MutationHookOptions<
    CreateAwsIamCredentialMutationData,
    CreateAwsIamCredentialMutationError,
    CreateAwsIamCredentialMutationVariables
  >,
): UseMutationResult<
  CreateAwsIamCredentialMutationData,
  CreateAwsIamCredentialMutationError,
  CreateAwsIamCredentialMutationVariables
>;
export declare function mutationKeyCreateAwsIamCredential(): MutationKey;
export declare function buildCreateAwsIamCredentialMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateAwsIamCredentialMutationVariables,
  ) => Promise<CreateAwsIamCredentialMutationData>;
};
//# sourceMappingURL=createAwsIamCredential.d.ts.map
