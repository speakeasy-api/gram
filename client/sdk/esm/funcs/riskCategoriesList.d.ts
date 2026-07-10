import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskCategoriesResult } from "../models/components/riskcategoriesresult.js";
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
  ListRiskCategoriesRequest,
  ListRiskCategoriesSecurity,
} from "../models/operations/listriskcategories.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listRiskCategories risk
 *
 * @remarks
 * Return the canonical risk category definitions: metadata (label/description/icon) plus the classification (source / rule_id list / rule_id prefix) used to bucket findings. Dashboards and CLIs should call this instead of maintaining their own copy of the mapping.
 */
export declare function riskCategoriesList(
  client: GramCore,
  request?: ListRiskCategoriesRequest | undefined,
  security?: ListRiskCategoriesSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RiskCategoriesResult,
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
//# sourceMappingURL=riskCategoriesList.d.ts.map
