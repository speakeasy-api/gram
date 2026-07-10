import {
  InvalidateQueryFilters,
  QueryClient,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
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
  GetProjectRequest,
  GetProjectSecurity,
} from "../models/operations/getproject.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildProjectQuery,
  prefetchProject,
  ProjectQueryData,
  queryKeyProject,
} from "./project.core.js";
export {
  buildProjectQuery,
  prefetchProject,
  type ProjectQueryData,
  queryKeyProject,
};
export type ProjectQueryError =
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
 * getProject projects
 *
 * @remarks
 * Get project details by slug.
 */
export declare function useProject(
  request: GetProjectRequest,
  security?: GetProjectSecurity | undefined,
  options?: QueryHookOptions<ProjectQueryData, ProjectQueryError>,
): UseQueryResult<ProjectQueryData, ProjectQueryError>;
/**
 * getProject projects
 *
 * @remarks
 * Get project details by slug.
 */
export declare function useProjectSuspense(
  request: GetProjectRequest,
  security?: GetProjectSecurity | undefined,
  options?: SuspenseQueryHookOptions<ProjectQueryData, ProjectQueryError>,
): UseSuspenseQueryResult<ProjectQueryData, ProjectQueryError>;
export declare function setProjectData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      slug: string;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: ProjectQueryData,
): ProjectQueryData | undefined;
export declare function invalidateProject(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        slug: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllProject(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=project.d.ts.map
