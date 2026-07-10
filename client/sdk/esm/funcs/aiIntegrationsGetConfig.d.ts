import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AIIntegrationConfig } from "../models/components/aiintegrationconfig.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetAIIntegrationConfigRequest, GetAIIntegrationConfigSecurity } from "../models/operations/getaiintegrationconfig.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getConfig aiIntegrations
 *
 * @remarks
 * Get the org-wide AI integration config for a provider. Returns an empty config (enabled=false, has_api_key=false) when none is set.
 */
export declare function aiIntegrationsGetConfig(client: GramCore, request: GetAIIntegrationConfigRequest, security?: GetAIIntegrationConfigSecurity | undefined, options?: RequestOptions): APIPromise<Result<AIIntegrationConfig, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=aiIntegrationsGetConfig.d.ts.map