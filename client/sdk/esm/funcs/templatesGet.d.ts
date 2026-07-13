import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetPromptTemplateResult } from "../models/components/getprompttemplateresult.js";
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
  GetTemplateRequest,
  GetTemplateSecurity,
} from "../models/operations/gettemplate.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getTemplate templates
 *
 * @remarks
 * Get prompt template by its ID or name.
 */
export declare function templatesGet(
  client: GramCore,
  request?: GetTemplateRequest | undefined,
  security?: GetTemplateSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    GetPromptTemplateResult,
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
//# sourceMappingURL=templatesGet.d.ts.map
