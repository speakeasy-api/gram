import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AIIntegrationConfig } from "../models/components/aiintegrationconfig.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpsertAIIntegrationConfigRequest, UpsertAIIntegrationConfigSecurity } from "../models/operations/upsertaiintegrationconfig.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * upsertConfig aiIntegrations
 *
 * @remarks
 * Create or update the org-wide AI integration config for a provider.
 */
export declare function aiIntegrationsUpsertConfig(client: GramCore, request: UpsertAIIntegrationConfigRequest, security?: UpsertAIIntegrationConfigSecurity | undefined, options?: RequestOptions): APIPromise<Result<AIIntegrationConfig, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=aiIntegrationsUpsertConfig.d.ts.map