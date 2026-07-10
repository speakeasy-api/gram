import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetPluginsResult } from "../models/components/getpluginsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetAgentPluginsRequest, GetAgentPluginsSecurity } from "../models/operations/getagentplugins.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getPlugins agent
 *
 * @remarks
 * Resolve the marketplaces and plugins assigned to the enrolled user. The device agent reconciles these into whichever AI developer tools it manages (Claude Code today), so each tool's own plugin manager fetches and installs the bundles. The response is tool-agnostic: it names what to install, and each tool's syncer decides how to render it into that tool's native configuration.
 */
export declare function agentGetPlugins(client: GramCore, request: GetAgentPluginsRequest, security?: GetAgentPluginsSecurity | undefined, options?: RequestOptions): APIPromise<Result<GetPluginsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=agentGetPlugins.d.ts.map