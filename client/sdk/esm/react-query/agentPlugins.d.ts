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
  GetAgentPluginsRequest,
  GetAgentPluginsSecurity,
} from "../models/operations/getagentplugins.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  AgentPluginsQueryData,
  buildAgentPluginsQuery,
  prefetchAgentPlugins,
  queryKeyAgentPlugins,
} from "./agentPlugins.core.js";
export {
  type AgentPluginsQueryData,
  buildAgentPluginsQuery,
  prefetchAgentPlugins,
  queryKeyAgentPlugins,
};
export type AgentPluginsQueryError =
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
 * getPlugins agent
 *
 * @remarks
 * Resolve the marketplaces and plugins assigned to the enrolled user. The device agent reconciles these into whichever AI developer tools it manages (Claude Code today), so each tool's own plugin manager fetches and installs the bundles. The response is tool-agnostic: it names what to install, and each tool's syncer decides how to render it into that tool's native configuration.
 */
export declare function useAgentPlugins(
  request: GetAgentPluginsRequest,
  security?: GetAgentPluginsSecurity | undefined,
  options?: QueryHookOptions<AgentPluginsQueryData, AgentPluginsQueryError>,
): UseQueryResult<AgentPluginsQueryData, AgentPluginsQueryError>;
/**
 * getPlugins agent
 *
 * @remarks
 * Resolve the marketplaces and plugins assigned to the enrolled user. The device agent reconciles these into whichever AI developer tools it manages (Claude Code today), so each tool's own plugin manager fetches and installs the bundles. The response is tool-agnostic: it names what to install, and each tool's syncer decides how to render it into that tool's native configuration.
 */
export declare function useAgentPluginsSuspense(
  request: GetAgentPluginsRequest,
  security?: GetAgentPluginsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    AgentPluginsQueryData,
    AgentPluginsQueryError
  >,
): UseSuspenseQueryResult<AgentPluginsQueryData, AgentPluginsQueryError>;
export declare function setAgentPluginsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      email: string;
      gramKey?: string | undefined;
    },
  ],
  data: AgentPluginsQueryData,
): AgentPluginsQueryData | undefined;
export declare function invalidateAgentPlugins(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        email: string;
        gramKey?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllAgentPlugins(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=agentPlugins.d.ts.map
