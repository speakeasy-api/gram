import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RenderTemplateResult } from "../models/components/rendertemplateresult.js";
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
  RenderTemplateRequest,
  RenderTemplateSecurity,
} from "../models/operations/rendertemplate.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * renderTemplate templates
 *
 * @remarks
 * Render a prompt template directly with all template fields provided.
 */
export declare function templatesRender(
  client: GramCore,
  request: RenderTemplateRequest,
  security?: RenderTemplateSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RenderTemplateResult,
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
//# sourceMappingURL=templatesRender.d.ts.map
