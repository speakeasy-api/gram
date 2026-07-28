import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SetProjectLogoResult } from "../models/components/setprojectlogoresult.js";
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
  SetProjectLogoRequest,
  SetProjectLogoSecurity,
} from "../models/operations/setprojectlogo.js";
import { MutationHookOptions } from "./_types.js";
export type SetProjectLogoMutationVariables = {
  request: SetProjectLogoRequest;
  security?: SetProjectLogoSecurity | undefined;
  options?: RequestOptions;
};
export type SetProjectLogoMutationData = SetProjectLogoResult;
export type SetProjectLogoMutationError =
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
 * setLogo projects
 *
 * @remarks
 * Uploads a logo for a project.
 */
export declare function useSetProjectLogoMutation(
  options?: MutationHookOptions<
    SetProjectLogoMutationData,
    SetProjectLogoMutationError,
    SetProjectLogoMutationVariables
  >,
): UseMutationResult<
  SetProjectLogoMutationData,
  SetProjectLogoMutationError,
  SetProjectLogoMutationVariables
>;
export declare function mutationKeySetProjectLogo(): MutationKey;
export declare function buildSetProjectLogoMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: SetProjectLogoMutationVariables,
  ) => Promise<SetProjectLogoMutationData>;
};
//# sourceMappingURL=setProjectLogo.d.ts.map
