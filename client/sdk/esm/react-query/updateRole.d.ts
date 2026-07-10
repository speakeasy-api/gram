import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Role } from "../models/components/role.js";
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
  UpdateRoleRequest,
  UpdateRoleSecurity,
} from "../models/operations/updaterole.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateRoleMutationVariables = {
  request: UpdateRoleRequest;
  security?: UpdateRoleSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateRoleMutationData = Role;
export type UpdateRoleMutationError =
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
 * updateRole access
 *
 * @remarks
 * Update an existing custom role.
 */
export declare function useUpdateRoleMutation(
  options?: MutationHookOptions<
    UpdateRoleMutationData,
    UpdateRoleMutationError,
    UpdateRoleMutationVariables
  >,
): UseMutationResult<
  UpdateRoleMutationData,
  UpdateRoleMutationError,
  UpdateRoleMutationVariables
>;
export declare function mutationKeyUpdateRole(): MutationKey;
export declare function buildUpdateRoleMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateRoleMutationVariables,
  ) => Promise<UpdateRoleMutationData>;
};
//# sourceMappingURL=updateRole.d.ts.map
