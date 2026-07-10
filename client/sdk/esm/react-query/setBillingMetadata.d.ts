import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TokensUnderManagement } from "../models/components/tokensundermanagement.js";
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
  SetBillingMetadataRequest,
  SetBillingMetadataSecurity,
} from "../models/operations/setbillingmetadata.js";
import { MutationHookOptions } from "./_types.js";
export type SetBillingMetadataMutationVariables = {
  request: SetBillingMetadataRequest;
  security?: SetBillingMetadataSecurity | undefined;
  options?: RequestOptions;
};
export type SetBillingMetadataMutationData = TokensUnderManagement;
export type SetBillingMetadataMutationError =
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
 * setBillingMetadata usage
 *
 * @remarks
 * Set an organization's billing contract terms. Restricted to platform admins.
 */
export declare function useSetBillingMetadataMutation(
  options?: MutationHookOptions<
    SetBillingMetadataMutationData,
    SetBillingMetadataMutationError,
    SetBillingMetadataMutationVariables
  >,
): UseMutationResult<
  SetBillingMetadataMutationData,
  SetBillingMetadataMutationError,
  SetBillingMetadataMutationVariables
>;
export declare function mutationKeySetBillingMetadata(): MutationKey;
export declare function buildSetBillingMetadataMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: SetBillingMetadataMutationVariables,
  ) => Promise<SetBillingMetadataMutationData>;
};
//# sourceMappingURL=setBillingMetadata.d.ts.map
