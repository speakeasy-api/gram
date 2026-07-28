import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GcpIamCredential } from "../models/components/gcpiamcredential.js";
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
  CreateGcpIamCredentialRequest,
  CreateGcpIamCredentialSecurity,
} from "../models/operations/creategcpiamcredential.js";
import { MutationHookOptions } from "./_types.js";
export type CreateGcpIamCredentialMutationVariables = {
  request: CreateGcpIamCredentialRequest;
  security?: CreateGcpIamCredentialSecurity | undefined;
  options?: RequestOptions;
};
export type CreateGcpIamCredentialMutationData = GcpIamCredential;
export type CreateGcpIamCredentialMutationError =
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
 * createGcpIamCredential externalCredentials
 *
 * @remarks
 * Create a GCP IAM external credential. Requires org:admin.
 */
export declare function useCreateGcpIamCredentialMutation(
  options?: MutationHookOptions<
    CreateGcpIamCredentialMutationData,
    CreateGcpIamCredentialMutationError,
    CreateGcpIamCredentialMutationVariables
  >,
): UseMutationResult<
  CreateGcpIamCredentialMutationData,
  CreateGcpIamCredentialMutationError,
  CreateGcpIamCredentialMutationVariables
>;
export declare function mutationKeyCreateGcpIamCredential(): MutationKey;
export declare function buildCreateGcpIamCredentialMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateGcpIamCredentialMutationVariables,
  ) => Promise<CreateGcpIamCredentialMutationData>;
};
//# sourceMappingURL=createGcpIamCredential.d.ts.map
