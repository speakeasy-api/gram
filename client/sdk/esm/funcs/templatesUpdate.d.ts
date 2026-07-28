import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UpdatePromptTemplateResult } from "../models/components/updateprompttemplateresult.js";
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
  UpdateTemplateRequest,
  UpdateTemplateSecurity,
} from "../models/operations/updatetemplate.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateTemplate templates
 *
 * @remarks
 * Update a prompt template.
 */
export declare function templatesUpdate(
  client: GramCore,
  request: UpdateTemplateRequest,
  security?: UpdateTemplateSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    UpdatePromptTemplateResult,
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
//# sourceMappingURL=templatesUpdate.d.ts.map
