import { Page } from "@/components/page-layout";
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
import UploadAssetStepper, {
  useStepper,
} from "@/components/upload-asset/stepper";
import UploadFileStep from "@/components/upload-asset/upload-file-step";
import { useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useListTools } from "@/hooks/toolTypes";
import { slugify } from "@/lib/constants";
import { cn, getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  Deployment,
  GetDeploymentResult,
  UploadOpenAPIv3Result,
} from "@gram/client/models/components";
import {
  useDeploymentLogs,
  useLatestDeployment,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { Heading } from "@/components/ui/heading";
import { Alert, Button, CodeSnippet, Stack } from "@speakeasy-api/moonshine";
import {
  ArrowRightIcon,
  CheckIcon,
  FileTextIcon,
  RefreshCcwIcon,
} from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router";

export default function UploadOpenAPI() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="max-w-2xl">
          {/* Header */}
          <Stack gap={3} className="mb-8">
            <Stack direction="horizontal" gap={3} align="center">
              <div className="w-10 h-10 rounded-lg bg-blue-500/10 flex items-center justify-center shrink-0">
                <FileTextIcon className="w-5 h-5 text-blue-600 dark:text-blue-400" />
              </div>
              <Heading variant="h3">Import OpenAPI Specification</Heading>
            </Stack>
            <Type muted>
              Upload your OpenAPI spec to automatically generate tools for every
              endpoint. Supports JSON and YAML formats.
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
        <Button variant="primary" onClick={() => routes.toolsets.goTo()}>
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

export function useUploadOpenAPISteps(checkDocumentSlugUnique = true) {
  const project = useProject();
  const session = useSession();
  const client = useSdkClient();
  const telemetry = useTelemetry();

  const { data: latestDeployment } = useLatestDeployment();

  const [file, setFile] = useState<File>();
  const [asset, setAsset] = useState<UploadOpenAPIv3Result>();
  const [creatingDeployment, setCreatingDeployment] = useState(false);
  const [apiName, setApiName] = useState<string | undefined>();
  const [deployment, setDeployment] = useState<Deployment>();

  // If an existing document slug was NOT provided, then we need to make sure the provided slug
  // isn't accidentally overwriting an existing document slug.
  let apiNameError: string | undefined;

  if (apiName) {
    if (apiName.length < 3) {
      apiNameError = "API name must be at least 3 characters long";
    }

    if (
      checkDocumentSlugUnique &&
      latestDeployment?.deployment?.openapiv3Assets
        .map((a) => a.slug)
        .includes(apiName)
    ) {
      apiNameError = "API name must be unique";
    }
  } else {
    apiNameError = "API name is required";
  }

  const getContentType = (file: File) => {
    if (file.type) return file.type;
    const ext = file.name.split(".").pop()?.toLowerCase();
    switch (ext) {
      case "json":
        return "application/json";
      case "yaml":
      case "yml":
        return "application/yaml";
      default:
        return "application/octet-stream";
    }
  };

  const handleSpecUpload = async (file: File) => {
    try {
      setFile(file);

      telemetry.capture("onboarding_event", {
        action: "spec_uploaded",
      });

      // Need to use fetch directly because the SDK doesn't support file uploads
      fetch(`${getServerURL()}/rpc/assets.uploadOpenAPIv3`, {
        method: "POST",
        headers: {
          "content-type": getContentType(file),
          "content-length": file.size.toString(),
          "gram-session": session.session,
          "gram-project": project.slug,
        },
        body: file,
      }).then(async (response) => {
        if (!response.ok) {
          throw new Error(`Upload failed`);
        }

        const result: UploadOpenAPIv3Result = await response.json();

        setAsset(result);
        if (!apiName) {
          setApiName(slugify(file?.name.split(".")[0] ?? "My API"));
        }
      });
    } catch (_error) {
      // Error will be shown to user via toast notifications
    }
  };

  const handleUrlUpload = (result: UploadOpenAPIv3Result) => {
    setAsset(result);
    setFile(
      new File([], "My API", {
        type: result.asset?.contentType ?? "application/yaml",
      }),
    );
    telemetry.capture("onboarding_event", {
      action: "spec_uploaded",
      source: "url",
    });
    if (!apiName) {
      setApiName("My API");
    }
  };

  const createDeployment = async (documentSlug?: string, forceNew = false) => {
    if (!asset || !apiName) {
      throw new Error("Asset or file not found");
    }

    setCreatingDeployment(true);

    const shouldCreateNew =
      !latestDeployment ||
      (forceNew && latestDeployment.deployment?.openapiv3ToolCount === 0);

    let deployment: Deployment | undefined;
    if (shouldCreateNew) {
      const createResult = await client.deployments.create({
        idempotencyKey: crypto.randomUUID(),
        createDeploymentRequestBody: {
          openapiv3Assets: [
            {
              assetId: asset.asset.id,
              name: documentSlug ?? apiName,
              slug: documentSlug ?? slugify(apiName),
            },
          ],
        },
      });

      deployment = createResult.deployment;
    } else {
      const createResult = await client.deployments.evolveDeployment({
        evolveForm: {
          upsertOpenapiv3Assets: [
            {
              assetId: asset.asset.id,
              name: documentSlug ?? apiName,
              slug: documentSlug ?? slugify(apiName),
            },
          ],
        },
      });

      deployment = createResult.deployment;
    }

    if (!deployment) {
      throw new Error("Deployment not found");
    }

    // Wait for deployment to finish
    while (
      deployment.status !== "completed" &&
      deployment.status !== "failed"
    ) {
      await new Promise((resolve) => setTimeout(resolve, 100));
      deployment = await client.deployments.getById({
        id: deployment.id,
      });
    }

    setDeployment(deployment);
    setCreatingDeployment(false);

    if (deployment.status === "failed") {
      telemetry.capture("onboarding_event", {
        action: "deployment_failed",
      });
    } else {
      telemetry.capture("onboarding_event", {
        action: "deployment_created",
        num_tools: deployment?.openapiv3ToolCount,
      });
    }

    if (deployment?.openapiv3ToolCount === 0) {
      telemetry.capture("onboarding_event", {
        action: "no_tools_found",
        error: "no_tools_found",
      });
    }

    return deployment;
  };

  const undoSpecUpload = () => {
    setFile(undefined);
    setAsset(undefined);
    setApiName(undefined);
  };

  return {
    apiNameError,
    handleSpecUpload,
    handleUrlUpload,
    undoSpecUpload,
    apiName,
    setApiName,
    createDeployment,
    file,
    asset,
    createdDeployment: deployment,
    creatingDeployment,
  };
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
            className="max-w-sm relative z-10"
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
          "py-1 px-4 dark:hover:bg-white/15 rounded hover:bg-gray-100",
        )}
      >
        {e.message}
      </p>
    );
  });

  return (
    <div className="font-mono text-sm max-h-[250px] overflow-y-auto">
      {lines.length > 0 ? lines : "OpenAPI document processed without issue"}
    </div>
  );
}

export function useIsProjectEmpty() {
  const { projectSlug } = useParams();

  const { data: deployment, isLoading: isDeploymentLoading } =
    useLatestDeployment({ gramProject: projectSlug });
  const { data: toolsets, isLoading: isToolsetsLoading } = useListToolsets({
    gramProject: projectSlug,
  });

  return {
    isLoading: isDeploymentLoading || isToolsetsLoading,
    isEmpty:
      isDeploymentEmpty(deployment?.deployment) &&
      toolsets?.toolsets.length === 0,
  };
}

function isDeploymentEmpty(deployment: GetDeploymentResult | undefined) {
  return (
    !deployment ||
    (deployment?.openapiv3Assets.length === 0 &&
      (deployment.functionsAssets?.length ?? 0) === 0 &&
      deployment?.packages.length === 0)
  );
}
