import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListPromptTemplatesResult } from "../models/components/listprompttemplatesresult.js";
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
  ListTemplatesRequest,
  ListTemplatesSecurity,
} from "../models/operations/listtemplates.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listTemplates templates
 *
 * @remarks
 * List available prompt template.
 */
export declare function templatesList(
  client: GramCore,
  request?: ListTemplatesRequest | undefined,
  security?: ListTemplatesSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListPromptTemplatesResult,
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
//# sourceMappingURL=templatesList.d.ts.map
