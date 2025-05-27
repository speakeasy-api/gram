import { Page } from "@/components/page-layout";
import FileUpload from "@/components/upload";
import { Type } from "@/components/ui/type";
import { CodeSnippet, Stack } from "@speakeasy-api/moonshine";
import { useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import {
  Deployment,
  UploadOpenAPIv3Result,
} from "@gram/client/models/components";
import { useNavigate } from "react-router";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  useDeploymentLogs,
  useLatestDeployment,
  useListTools,
} from "@gram/client/react-query/index.js";
import { Input } from "@/components/ui/input";
import { Stepper, StepProps } from "@/components/stepper";
import { cn, getServerURL } from "@/lib/utils";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";

export default function Onboarding() {
  const navigate = useNavigate();

  const onOnboardingComplete = () => {
    navigate("/toolsets");
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <OnboardingContent onOnboardingComplete={onOnboardingComplete} />
    </Page>
  );
}

export function OnboardingContent({
  existingDocumentSlug,
  onOnboardingComplete,
  className,
}: {
  existingDocumentSlug?: string;
  onOnboardingComplete?: (deployment: Deployment) => void;
  className?: string;
}) {
  const project = useProject();
  const session = useSession();
  const client = useSdkClient();

  const { data: latestDeployment } = useLatestDeployment();

  const [file, setFile] = useState<File>();
  const [asset, setAsset] = useState<UploadOpenAPIv3Result>();
  const [creatingDeployment, setCreatingDeployment] = useState(false);
  const [apiName, setApiName] = useState<string | undefined>(
    existingDocumentSlug
  );
  const [deployment, setDeployment] = useState<Deployment>();

  const { data: tools, refetch: refetchTools } = useListTools();

  const handleUpload = async (file: File) => {
    try {
      setFile(file);

      // Need to use fetch directly because the SDK doesn't support file uploads
      fetch(`${getServerURL()}/rpc/assets.uploadOpenAPIv3`, {
        method: "POST",
        headers: {
          "content-type": file.type,
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
          setApiName(file?.name.split(".")[0] ?? "My API");
        }
      });
    } catch (error) {
      console.error("Upload failed:", error);
    }
  };

  const createDeployment = async () => {
    if (!asset || !apiName) {
      throw new Error("Asset or file not found");
    }

    setCreatingDeployment(true);

    const deployment = await client.deployments.evolveDeployment({
      evolveForm: {
        upsertOpenapiv3Assets: [
          {
            assetId: asset.asset.id,
            name: apiName,
            slug: apiName.replace(" ", "-").toLowerCase(),
          },
        ],
      },
    });

    setDeployment(deployment.deployment);
    refetchTools();
    setCreatingDeployment(false);
  };

  const documentId = deployment?.openapiv3Assets.find(
    (doc) => doc.assetId === asset?.asset.id
  )?.id;

  const numTools = documentId
    ? tools?.tools.filter(
        (tool) =>
          tool.openapiv3DocumentId !== undefined &&
          tool.deploymentId === deployment?.id &&
          tool.openapiv3DocumentId === documentId
      ).length
    : 0;

  // If an existing document slug was NOT provided, then we need to make sure the provided slug
  // isn't accidentally overwriting an existing document slug.
  const apiNameValid =
    apiName &&
    apiName.length > 0 &&
    (existingDocumentSlug ||
      !latestDeployment?.deployment?.openapiv3Assets
        .map((a) => a.slug)
        .includes(apiName));

  const steps: StepProps[] = [
    {
      heading: "Upload OpenAPI Specification",
      description: "Upload your OpenAPI specification to get started.",
      display: (
        <FileUpload
          onUpload={handleUpload}
          allowedExtensions={["yaml", "yml", "json"]}
        />
      ),
      displayComplete: (
        <Stack direction={"horizontal"} gap={2} align={"center"}>
          <Type>✓ Uploaded {file?.name}</Type>
          <Button
            variant={"outline"}
            onClick={() => {
              setFile(undefined);
              setAsset(undefined);
            }}
          >
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
          <Stack direction={"horizontal"} gap={2} className="max-w-sm">
            <Input
              value={apiName}
              onChange={(e) => setApiName(e.target.value)}
              placeholder="My API"
              disabled={!!existingDocumentSlug}
            />
            <Button onClick={createDeployment} disabled={!apiNameValid}>
              Continue
            </Button>
          </Stack>
          {!!apiName && !apiNameValid && (
            <span className="text-destructive">
              This slug is already in use.
            </span>
          )}
        </Stack>
      ),
      displayComplete: <Type>✓ Source named "{apiName}"</Type>,
      isComplete: creatingDeployment || !!deployment,
    },
    {
      heading: "Generate Tools",
      description: "Gram will generate tools for your API.",
      display: numTools ? (
        <Type>✓ Created {numTools} tools</Type>
      ) : (
        <Type>
          Gram is generating tools for your API. This may take a few seconds.
        </Type>
      ),
      displayComplete: (
        <div>
          {deployment ? (
            <Accordion type="single" collapsible>
              <AccordionItem value="logs">
                <AccordionTrigger className="text-base">
                  ✓ Created {numTools} tools
                </AccordionTrigger>
                <AccordionContent>
                  <DeploymentLogs deploymentId={deployment?.id} />
                </AccordionContent>
              </AccordionItem>
            </Accordion>
          ) : null}
        </div>
      ),
      isComplete: !!deployment,
    },
  ];

  return (
    <Page.Body className={className}>
      <Stepper
        steps={steps}
        onComplete={() => onOnboardingComplete?.(deployment!)}
      />
    </Page.Body>
  );
}

function DeploymentLogs(props: { deploymentId: string }) {
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

  const lines = data.events.map((e) => {
    return (
      <p
        key={e.id}
        className={cn(
          e.event.includes("error") && "text-destructive",
          "py-1 px-4 dark:hover:bg-white/15 rounded hover:bg-gray-100"
        )}
      >
        {e.message}
      </p>
    );
  });

  return <div className="font-mono text-sm">{lines}</div>;
}
