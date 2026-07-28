import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  DeleteProjectRequest,
  DeleteProjectSecurity,
} from "../models/operations/deleteproject.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteProjectMutationVariables = {
  request: DeleteProjectRequest;
  security?: DeleteProjectSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteProjectMutationData = void;
export type DeleteProjectMutationError =
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
 * deleteProject projects
 *
 * @remarks
 * Delete a project by its ID
 */
export declare function useDeleteProjectMutation(
  options?: MutationHookOptions<
    DeleteProjectMutationData,
    DeleteProjectMutationError,
    DeleteProjectMutationVariables
  >,
): UseMutationResult<
  DeleteProjectMutationData,
  DeleteProjectMutationError,
  DeleteProjectMutationVariables
>;
export declare function mutationKeyDeleteProject(): MutationKey;
export declare function buildDeleteProjectMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteProjectMutationVariables,
  ) => Promise<DeleteProjectMutationData>;
};
//# sourceMappingURL=deleteProject.d.ts.map
