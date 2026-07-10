import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { CreateDeploymentResult } from "../models/components/createdeploymentresult.js";
import { EvolveResult } from "../models/components/evolveresult.js";
import { GetActiveDeploymentResult } from "../models/components/getactivedeploymentresult.js";
import { GetDeploymentLogsResult } from "../models/components/getdeploymentlogsresult.js";
import { GetDeploymentResult } from "../models/components/getdeploymentresult.js";
import { GetLatestDeploymentResult } from "../models/components/getlatestdeploymentresult.js";
import { ListDeploymentResult } from "../models/components/listdeploymentresult.js";
import { RedeployResult } from "../models/components/redeployresult.js";
import { CreateDeploymentRequest, CreateDeploymentSecurity } from "../models/operations/createdeployment.js";
import { EvolveDeploymentRequest, EvolveDeploymentSecurity } from "../models/operations/evolvedeployment.js";
import { GetActiveDeploymentRequest, GetActiveDeploymentSecurity } from "../models/operations/getactivedeployment.js";
import { GetDeploymentRequest, GetDeploymentSecurity } from "../models/operations/getdeployment.js";
import { GetDeploymentLogsRequest, GetDeploymentLogsSecurity } from "../models/operations/getdeploymentlogs.js";
import { GetLatestDeploymentRequest, GetLatestDeploymentSecurity } from "../models/operations/getlatestdeployment.js";
import { ListDeploymentsRequest, ListDeploymentsSecurity } from "../models/operations/listdeployments.js";
import { RedeployDeploymentRequest, RedeployDeploymentSecurity } from "../models/operations/redeploydeployment.js";
export declare class Deployments extends ClientSDK {
    /**
     * getActiveDeployment deployments
     *
     * @remarks
     * Get the active deployment for a project.
     */
    active(request?: GetActiveDeploymentRequest | undefined, security?: GetActiveDeploymentSecurity | undefined, options?: RequestOptions): Promise<GetActiveDeploymentResult>;
    /**
     * createDeployment deployments
     *
     * @remarks
     * Create a deployment to load tool definitions.
     */
    create(request: CreateDeploymentRequest, security?: CreateDeploymentSecurity | undefined, options?: RequestOptions): Promise<CreateDeploymentResult>;
    /**
     * evolve deployments
     *
     * @remarks
     * Create a new deployment with additional or updated tool sources.
     */
    evolveDeployment(request: EvolveDeploymentRequest, security?: EvolveDeploymentSecurity | undefined, options?: RequestOptions): Promise<EvolveResult>;
    /**
     * getDeployment deployments
     *
     * @remarks
     * Get a deployment by its ID.
     */
    getById(request: GetDeploymentRequest, security?: GetDeploymentSecurity | undefined, options?: RequestOptions): Promise<GetDeploymentResult>;
    /**
     * getLatestDeployment deployments
     *
     * @remarks
     * Get the latest deployment for a project.
     */
    latest(request?: GetLatestDeploymentRequest | undefined, security?: GetLatestDeploymentSecurity | undefined, options?: RequestOptions): Promise<GetLatestDeploymentResult>;
    /**
     * listDeployments deployments
     *
     * @remarks
     * List all deployments in descending order of creation.
     */
    list(request?: ListDeploymentsRequest | undefined, security?: ListDeploymentsSecurity | undefined, options?: RequestOptions): Promise<ListDeploymentResult>;
    /**
     * getDeploymentLogs deployments
     *
     * @remarks
     * Get logs for a deployment.
     */
    logs(request: GetDeploymentLogsRequest, security?: GetDeploymentLogsSecurity | undefined, options?: RequestOptions): Promise<GetDeploymentLogsResult>;
    /**
     * redeploy deployments
     *
     * @remarks
     * Redeploys an existing deployment.
     */
    redeployDeployment(request: RedeployDeploymentRequest, security?: RedeployDeploymentSecurity | undefined, options?: RequestOptions): Promise<RedeployResult>;
}
//# sourceMappingURL=deployments.d.ts.map