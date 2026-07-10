import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteGcpIamCredentialRequest, DeleteGcpIamCredentialSecurity } from "../models/operations/deletegcpiamcredential.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteGcpIamCredentialMutationVariables = {
    request: DeleteGcpIamCredentialRequest;
    security?: DeleteGcpIamCredentialSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteGcpIamCredentialMutationData = void;
export type DeleteGcpIamCredentialMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteGcpIamCredential externalCredentials
 *
 * @remarks
 * Soft-delete a GCP IAM external credential by ID. Requires org:admin.
 */
export declare function useDeleteGcpIamCredentialMutation(options?: MutationHookOptions<DeleteGcpIamCredentialMutationData, DeleteGcpIamCredentialMutationError, DeleteGcpIamCredentialMutationVariables>): UseMutationResult<DeleteGcpIamCredentialMutationData, DeleteGcpIamCredentialMutationError, DeleteGcpIamCredentialMutationVariables>;
export declare function mutationKeyDeleteGcpIamCredential(): MutationKey;
export declare function buildDeleteGcpIamCredentialMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteGcpIamCredentialMutationVariables) => Promise<DeleteGcpIamCredentialMutationData>;
};
//# sourceMappingURL=deleteGcpIamCredential.d.ts.map