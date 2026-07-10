import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GenerateWorkOSAdminPortalLinkResult } from "../models/components/generateworkosadminportallinkresult.js";
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
  GenerateWorkOSAdminPortalLinkRequest,
  GenerateWorkOSAdminPortalLinkSecurity,
} from "../models/operations/generateworkosadminportallink.js";
import { MutationHookOptions } from "./_types.js";
export type GenerateWorkOSAdminPortalLinkMutationVariables = {
  request: GenerateWorkOSAdminPortalLinkRequest;
  security?: GenerateWorkOSAdminPortalLinkSecurity | undefined;
  options?: RequestOptions;
};
export type GenerateWorkOSAdminPortalLinkMutationData =
  GenerateWorkOSAdminPortalLinkResult;
export type GenerateWorkOSAdminPortalLinkMutationError =
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
 * generateWorkOSAdminPortalLink organizations
 *
 * @remarks
 * Generate a WorkOS Admin Portal link for the given intent (e.g. dsync, sso).
 */
export declare function useGenerateWorkOSAdminPortalLinkMutation(
  options?: MutationHookOptions<
    GenerateWorkOSAdminPortalLinkMutationData,
    GenerateWorkOSAdminPortalLinkMutationError,
    GenerateWorkOSAdminPortalLinkMutationVariables
  >,
): UseMutationResult<
  GenerateWorkOSAdminPortalLinkMutationData,
  GenerateWorkOSAdminPortalLinkMutationError,
  GenerateWorkOSAdminPortalLinkMutationVariables
>;
export declare function mutationKeyGenerateWorkOSAdminPortalLink(): MutationKey;
export declare function buildGenerateWorkOSAdminPortalLinkMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: GenerateWorkOSAdminPortalLinkMutationVariables,
  ) => Promise<GenerateWorkOSAdminPortalLinkMutationData>;
};
//# sourceMappingURL=generateWorkOSAdminPortalLink.d.ts.map
