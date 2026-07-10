import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CustomDomain } from "../models/components/customdomain.js";
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
  UpdateDomainRequest,
  UpdateDomainSecurity,
} from "../models/operations/updatedomain.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateDomainMutationVariables = {
  request: UpdateDomainRequest;
  security?: UpdateDomainSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateDomainMutationData = CustomDomain;
export type UpdateDomainMutationError =
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
 * updateDomain domains
 *
 * @remarks
 * Update the IP allowlist for the organization's custom domain
 */
export declare function useUpdateDomainMutation(
  options?: MutationHookOptions<
    UpdateDomainMutationData,
    UpdateDomainMutationError,
    UpdateDomainMutationVariables
  >,
): UseMutationResult<
  UpdateDomainMutationData,
  UpdateDomainMutationError,
  UpdateDomainMutationVariables
>;
export declare function mutationKeyUpdateDomain(): MutationKey;
export declare function buildUpdateDomainMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateDomainMutationVariables,
  ) => Promise<UpdateDomainMutationData>;
};
//# sourceMappingURL=updateDomain.d.ts.map
