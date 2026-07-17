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
  UpdateAwsIamCredentialRequest,
  UpdateAwsIamCredentialSecurity,
} from "../models/operations/updateawsiamcredential.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateAwsIamCredentialMutationVariables = {
  request: UpdateAwsIamCredentialRequest;
  security?: UpdateAwsIamCredentialSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateAwsIamCredentialMutationData = AwsIamCredential;
export type UpdateAwsIamCredentialMutationError =
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
 * updateAwsIamCredential externalCredentials
 *
 * @remarks
 * Replace an AWS IAM external credential's configuration. Requires org:admin.
 */
export declare function useUpdateAwsIamCredentialMutation(
  options?: MutationHookOptions<
    UpdateAwsIamCredentialMutationData,
    UpdateAwsIamCredentialMutationError,
    UpdateAwsIamCredentialMutationVariables
  >,
): UseMutationResult<
  UpdateAwsIamCredentialMutationData,
  UpdateAwsIamCredentialMutationError,
  UpdateAwsIamCredentialMutationVariables
>;
export declare function mutationKeyUpdateAwsIamCredential(): MutationKey;
export declare function buildUpdateAwsIamCredentialMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateAwsIamCredentialMutationVariables,
  ) => Promise<UpdateAwsIamCredentialMutationData>;
};
//# sourceMappingURL=updateAwsIamCredential.d.ts.map
