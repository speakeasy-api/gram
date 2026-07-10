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
  UpdateGcpIamCredentialRequest,
  UpdateGcpIamCredentialSecurity,
} from "../models/operations/updategcpiamcredential.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateGcpIamCredentialMutationVariables = {
  request: UpdateGcpIamCredentialRequest;
  security?: UpdateGcpIamCredentialSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateGcpIamCredentialMutationData = GcpIamCredential;
export type UpdateGcpIamCredentialMutationError =
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
 * updateGcpIamCredential externalCredentials
 *
 * @remarks
 * Replace a GCP IAM external credential's configuration. Requires org:admin.
 */
export declare function useUpdateGcpIamCredentialMutation(
  options?: MutationHookOptions<
    UpdateGcpIamCredentialMutationData,
    UpdateGcpIamCredentialMutationError,
    UpdateGcpIamCredentialMutationVariables
  >,
): UseMutationResult<
  UpdateGcpIamCredentialMutationData,
  UpdateGcpIamCredentialMutationError,
  UpdateGcpIamCredentialMutationVariables
>;
export declare function mutationKeyUpdateGcpIamCredential(): MutationKey;
export declare function buildUpdateGcpIamCredentialMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateGcpIamCredentialMutationVariables,
  ) => Promise<UpdateGcpIamCredentialMutationData>;
};
//# sourceMappingURL=updateGcpIamCredential.d.ts.map
