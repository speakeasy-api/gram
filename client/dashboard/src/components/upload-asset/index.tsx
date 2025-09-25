import NameDeploymentStep from "./name-deployment-step";
import UploadAssetStep from "./step";
import UploadAssetStepper from "./stepper";
import UploadFileStep from "./upload-file-step";

export default function UploadAsset() {
  return (
    <UploadAssetStepper.Root step={1}>
      <UploadAssetStep.Root step={1}>
        <UploadAssetStep.Indicator />
        <UploadAssetStep.Header
          title="Upload OpenAPI Specification"
          description="Upload your OpenAPI specification to get started."
        />
        <UploadAssetStep.Content>
          <UploadFileStep />
        </UploadAssetStep.Content>
      </UploadAssetStep.Root>

      <UploadAssetStep.Root step={2}>
        <UploadAssetStep.Indicator />
        <UploadAssetStep.Header
          title="Name Your API"
          description="The tools generated will be scoped under this name."
        />
        <UploadAssetStep.Content>
          <NameDeploymentStep />
        </UploadAssetStep.Content>
      </UploadAssetStep.Root>
    </UploadAssetStepper.Root>
  );
}
