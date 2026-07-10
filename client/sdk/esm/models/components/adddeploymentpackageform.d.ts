import * as z from "zod/v4-mini";
export type AddDeploymentPackageForm = {
    /**
     * The name of the package.
     */
    name: string;
    /**
     * The version of the package.
     */
    version?: string | undefined;
};
/** @internal */
export type AddDeploymentPackageForm$Outbound = {
    name: string;
    version?: string | undefined;
};
/** @internal */
export declare const AddDeploymentPackageForm$outboundSchema: z.ZodMiniType<AddDeploymentPackageForm$Outbound, AddDeploymentPackageForm>;
export declare function addDeploymentPackageFormToJSON(addDeploymentPackageForm: AddDeploymentPackageForm): string;
//# sourceMappingURL=adddeploymentpackageform.d.ts.map