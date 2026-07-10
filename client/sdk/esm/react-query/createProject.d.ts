import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreateProjectResult } from "../models/components/createprojectresult.js";
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
  CreateProjectRequest,
  CreateProjectSecurity,
} from "../models/operations/createproject.js";
import { MutationHookOptions } from "./_types.js";
export type CreateProjectMutationVariables = {
  request: CreateProjectRequest;
  security?: CreateProjectSecurity | undefined;
  options?: RequestOptions;
};
export type CreateProjectMutationData = CreateProjectResult;
export type CreateProjectMutationError =
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
 * createProject projects
 *
 * @remarks
 * Create a new project.
 */
export declare function useCreateProjectMutation(
  options?: MutationHookOptions<
    CreateProjectMutationData,
    CreateProjectMutationError,
    CreateProjectMutationVariables
  >,
): UseMutationResult<
  CreateProjectMutationData,
  CreateProjectMutationError,
  CreateProjectMutationVariables
>;
export declare function mutationKeyCreateProject(): MutationKey;
export declare function buildCreateProjectMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateProjectMutationVariables,
  ) => Promise<CreateProjectMutationData>;
};
//# sourceMappingURL=createProject.d.ts.map
