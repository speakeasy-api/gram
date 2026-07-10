import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { GetProductFeaturesResponseBody } from "../models/components/getproductfeaturesresponsebody.js";
import { GetProductFeaturesRequest, GetProductFeaturesSecurity } from "../models/operations/getproductfeatures.js";
import { SetProductFeatureRequest, SetProductFeatureSecurity } from "../models/operations/setproductfeature.js";
export declare class Features extends ClientSDK {
    /**
     * getProductFeatures features
     *
     * @remarks
     * Get the current state of all product feature flags.
     */
    get(request?: GetProductFeaturesRequest | undefined, security?: GetProductFeaturesSecurity | undefined, options?: RequestOptions): Promise<GetProductFeaturesResponseBody>;
    /**
     * setProductFeature features
     *
     * @remarks
     * Enable or disable an organization feature flag.
     */
    set(request: SetProductFeatureRequest, security?: SetProductFeatureSecurity | undefined, options?: RequestOptions): Promise<void>;
}
//# sourceMappingURL=features.d.ts.map