import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { RiskCategoriesResult } from "../models/components/riskcategoriesresult.js";
import {
  ListRiskCategoriesRequest,
  ListRiskCategoriesSecurity,
} from "../models/operations/listriskcategories.js";
export declare class Categories extends ClientSDK {
  /**
   * listRiskCategories risk
   *
   * @remarks
   * Return the canonical risk category definitions: metadata (label/description/icon) plus the classification (source / rule_id list / rule_id prefix) used to bucket findings. Dashboards and CLIs should call this instead of maintaining their own copy of the mapping.
   */
  list(
    request?: ListRiskCategoriesRequest | undefined,
    security?: ListRiskCategoriesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskCategoriesResult>;
}
//# sourceMappingURL=categories.d.ts.map
