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
  DeleteAIIntegrationConfigRequest,
  DeleteAIIntegrationConfigSecurity,
} from "../models/operations/deleteaiintegrationconfig.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteConfig aiIntegrations
 *
 * @remarks
 * Delete the org-wide AI integration config for a provider.
 */
export declare function aiIntegrationsDeleteConfig(
  client: GramCore,
  request: DeleteAIIntegrationConfigRequest,
  security?: DeleteAIIntegrationConfigSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    void,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=aiIntegrationsDeleteConfig.d.ts.map
