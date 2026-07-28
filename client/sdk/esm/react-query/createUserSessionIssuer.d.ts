import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UserSessionIssuer } from "../models/components/usersessionissuer.js";
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
  CreateUserSessionIssuerRequest,
  CreateUserSessionIssuerSecurity,
} from "../models/operations/createusersessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type CreateUserSessionIssuerMutationVariables = {
  request: CreateUserSessionIssuerRequest;
  security?: CreateUserSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type CreateUserSessionIssuerMutationData = UserSessionIssuer;
export type CreateUserSessionIssuerMutationError =
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
 * createUserSessionIssuer userSessionIssuers
 *
 * @remarks
 * Create a new user_session_issuer.
 */
export declare function useCreateUserSessionIssuerMutation(
  options?: MutationHookOptions<
    CreateUserSessionIssuerMutationData,
    CreateUserSessionIssuerMutationError,
    CreateUserSessionIssuerMutationVariables
  >,
): UseMutationResult<
  CreateUserSessionIssuerMutationData,
  CreateUserSessionIssuerMutationError,
  CreateUserSessionIssuerMutationVariables
>;
export declare function mutationKeyCreateUserSessionIssuer(): MutationKey;
export declare function buildCreateUserSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateUserSessionIssuerMutationVariables,
  ) => Promise<CreateUserSessionIssuerMutationData>;
};
//# sourceMappingURL=createUserSessionIssuer.d.ts.map
