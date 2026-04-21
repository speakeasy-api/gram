import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Stepper, StepProps } from "@/components/stepper";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Type } from "@/components/ui/type";
import { FullWidthUpload } from "@/components/upload";
import DeployStep from "@/components/upload-asset/deploy-step";
import NameDeploymentStep from "@/components/upload-asset/name-deployment-step";
import UploadAssetStep from "@/components/upload-asset/step";
import UploadAssetStepper from "@/components/upload-asset/stepper";
import { useStepper } from "@/components/upload-asset/stepper/use-stepper";
import UploadFileStep from "@/components/upload-asset/upload-file-step";
import { useListTools } from "@/hooks/toolTypes";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Deployment } from "@gram/client/models/components";
import { useDeploymentLogs } from "@gram/client/react-query/index.js";
import { Heading } from "@/components/ui/heading";
import { Alert, Button, CodeSnippet, Stack } from "@speakeasy-api/moonshine";
import {
  ArrowRightIcon,
  CheckIcon,
  FileTextIcon,
  RefreshCcwIcon,
} from "lucide-react";
import { useUploadOpenAPISteps } from "./upload-openapi-utils";

export default function UploadOpenAPI() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="build:write" level="page">
          <div className="max-w-2xl">
            {/* Header */}
            <Stack gap={3} className="mb-8">
              <Stack direction="horizontal" gap={3} align="center">
                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-blue-500/10">
                  <FileTextIcon className="h-5 w-5 text-blue-600 dark:text-blue-400" />
                </div>
                <Heading variant="h3">Import OpenAPI Specification</Heading>
              </Stack>
              <Type muted>
                Upload your OpenAPI spec to automatically generate tools for
                every endpoint. Supports JSON and YAML formats.
              </Type>
            </Stack>

            {/* Stepper */}
            <UploadAssetStepper.Provider step={1}>
              <UploadAssetStepper.Frame>
                <UploadAssetStep step={1}>
                  <UploadAssetStep.Indicator />
                  <UploadAssetStep.Header
                    title="Upload OpenAPI Specification"
                    description="Upload your OpenAPI specification to get started."
                  />
                  <UploadAssetStep.Content>
                    <UploadFileStep />
                  </UploadAssetStep.Content>
                </UploadAssetStep>

                <UploadAssetStep step={2}>
                  <UploadAssetStep.Indicator />
                  <UploadAssetStep.Header
                    title="Name Your API"
                    description="The tools generated will be scoped under this name."
                  />
                  <UploadAssetStep.Content>
                    <NameDeploymentStep />
                  </UploadAssetStep.Content>
                </UploadAssetStep>

                <UploadAssetStep step={3}>
                  <UploadAssetStep.Indicator />
                  <UploadAssetStep.Header
                    title="Generate Tools"
                    description="Gram will generate tools for your API."
                  />
                  <UploadAssetStep.Content>
                    <DeployStep />
                  </UploadAssetStep.Content>
                </UploadAssetStep>

                <Stack direction="horizontal" justify="start">
                  <FooterActions />
                </Stack>
              </UploadAssetStepper.Frame>
            </UploadAssetStepper.Provider>

            {/* Help text */}
            <Type small muted className="mt-6">
              Don't have an OpenAPI spec?{" "}
              <a
                href="https://www.speakeasy.com/docs/gram"
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary hover:underline"
              >
                Learn how to create one
              </a>{" "}
              or try our sample specs.
            </Type>
          </div>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function FooterActions() {
  const stepper = useStepper();
  const routes = useRoutes();

  const deploymentId = stepper.meta.current.deployment?.id;

  switch (stepper.state) {
    case "idle":
      return null;
    case "completed":
      return (
        <Button variant="primary" onClick={() => routes.mcp.goTo()}>
          Continue
          <ArrowRightIcon className="size-4" />
        </Button>
      );
    case "error":
      if (!deploymentId) {
        // This should never happen, but just in case
        return (
          <Button variant="primary" onClick={stepper.reset}>
            <RefreshCcwIcon className="size-4" />
            Try Again
          </Button>
        );
      }

      return (
        <>
          <Button
            variant="primary"
            onClick={() => routes.deployments.deployment.goTo(deploymentId)}
          >
            View Logs
            <ArrowRightIcon className="size-4" />
          </Button>
        </>
      );
  }
}

const useAssetNumtools = (
  assetId: string | undefined,
  deployment: Deployment | undefined,
) => {
  const { data: tools } = useListTools({
    deploymentId: deployment?.id,
  });

  const documentId = deployment?.openapiv3Assets.find(
    (doc) => doc.assetId === assetId,
  )?.id;

  return documentId
    ? tools?.tools.filter(
        (tool) =>
          tool.type === "http" &&
          tool.openapiv3DocumentId !== undefined &&
          tool.deploymentId === deployment?.id &&
          tool.openapiv3DocumentId === documentId,
      ).length
    : 0;
};

export function UploadOpenAPIContent({
  onStepsComplete,
  className,
}: {
  onStepsComplete?: (deployment: Deployment) => void;
  className?: string;
}) {
  const {
    handleSpecUpload,
    undoSpecUpload,
    apiName,
    setApiName,
    createDeployment,
    createdDeployment,
    creatingDeployment,
    apiNameError,
    file,
    asset,
    isUploading,
  } = useUploadOpenAPISteps();
  const routes = useRoutes();

  const numtools = useAssetNumtools(asset?.asset.id, createdDeployment);

  const steps: StepProps[] = [
    {
      heading: "Upload OpenAPI Specification",
      description: "Upload your OpenAPI specification to get started.",
      display: (
        <FullWidthUpload
          onUpload={handleSpecUpload}
          allowedExtensions={["yaml", "yml", "json"]}
          isLoading={isUploading}
        />
      ),
      displayComplete: (
        <Stack direction={"horizontal"} gap={2} align={"center"}>
          <Type>✓ Uploaded {file?.name}</Type>
          <Button variant={"secondary"} onClick={undoSpecUpload}>
            Change
          </Button>
        </Stack>
      ),
      isComplete: !!asset,
    },
    {
      heading: "Name Your API",
      description: "The tools generated will be scoped under this name.",
      display: (
        <Stack gap={2}>
          <Stack
            direction={"horizontal"}
            gap={2}
            className="relative z-10 max-w-sm"
          >
            <Input value={apiName} onChange={setApiName} placeholder="My API" />
            <Button
              onClick={() => createDeployment()}
              disabled={!!apiNameError}
            >
              CONTINUE
            </Button>
          </Stack>
          {!!apiNameError && (
            <span className="text-destructive">{apiNameError}</span>
          )}
        </Stack>
      ),
      displayComplete: <Type>✓ Source named "{apiName}"</Type>,
      isComplete: creatingDeployment || !!createdDeployment,
    },
    {
      heading: "Generate Tools",
      description: "Gram will generate tools for your API.",
      display: (
        <>
          <Type>
            Gram is generating tools for your API. This may take a few seconds.
          </Type>
          <Spinner />
        </>
      ),
      get displayComplete() {
        if (!createdDeployment) return null;

        if (createdDeployment.status === "failed")
          return (
            <Alert variant="error" dismissible={false} className="text-sm">
              The deployment failed. Check the{" "}
              <routes.deployments.deployment.Link
                params={[createdDeployment.id]}
                className="text-link"
              >
                deployment logs
              </routes.deployments.deployment.Link>{" "}
              for more information.
            </Alert>
          );

        return (
          <div>
            {createdDeployment ? (
              <Accordion type="single" collapsible className="max-w-2xl">
                <AccordionItem value="logs">
                  <AccordionTrigger className="text-base">
                    <div className="flex items-center gap-2">
                      <CheckIcon className="size-4" /> Created {numtools} tools
                    </div>
                  </AccordionTrigger>
                  <AccordionContent>
                    <DeploymentLogs
                      deploymentId={createdDeployment?.id}
                      onlyErrors
                    />
                  </AccordionContent>
                </AccordionItem>
              </Accordion>
            ) : null}
          </div>
        );
      },
      isComplete: !!createdDeployment,
      failed: createdDeployment?.status === "failed",
    },
  ];

  return (
    <Page.Body className={className}>
      <Stepper
        steps={steps}
        onComplete={() => onStepsComplete?.(createdDeployment!)}
      />
    </Page.Body>
  );
}

export function DeploymentLogs(props: {
  deploymentId: string;
  onlyErrors?: boolean;
}) {
  const { data, status, error } = useDeploymentLogs({
    deploymentId: props.deploymentId,
  });

  if (status === "pending") {
    return <Type>Loading deployment logs...</Type>;
  }

  if (status === "error") {
    return (
      <div>
        <Type>Error loading deployment logs</Type>
        <CodeSnippet code={error.toString()} language="text" />
      </div>
    );
  }

  if (data == null) {
    return null;
  }

  const lines = (
    props.onlyErrors
      ? data.events.filter((e) => e.event.includes("error"))
      : data.events
  ).map((e) => {
    return (
      <p
        key={e.id}
        className={cn(
          e.event.includes("error") && "text-destructive",
          "rounded px-4 py-1 hover:bg-gray-100 dark:hover:bg-white/15",
        )}
      >
        {e.message}
      </p>
    );
  });

  return (
    <div className="max-h-[250px] overflow-y-auto font-mono text-sm">
      {lines.length > 0 ? lines : "OpenAPI document processed without issue"}
    </div>
  );
}
