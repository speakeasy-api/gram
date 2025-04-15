import { Page } from "@/components/page-layout";
import FileUpload from "@/components/ui/upload";
import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";
import { useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import {
  Deployment,
  UploadOpenAPIv3Result,
} from "@gram/client/models/components";
import { v4 as uuidv4 } from "uuid";
import { useNavigate } from "react-router-dom";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { useListTools } from "@gram/client/react-query/index.js";
import { Input } from "@/components/ui/input";
import { Stepper, StepProps } from "@/components/stepper";

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
  onOnboardingComplete,
}: {
  onOnboardingComplete?: (deployment: Deployment) => void;
}) {
  const project = useProject();
  const session = useSession();
  const client = useSdkClient();

  const [file, setFile] = useState<File>();
  const [asset, setAsset] = useState<UploadOpenAPIv3Result>();
  const [creatingDeployment, setCreatingDeployment] = useState(false);
  const [apiName, setApiName] = useState<string>();
  const [deployment, setDeployment] = useState<Deployment>();

  const { data: tools, refetch: refetchTools } = useListTools({
    gramProject: project.projectSlug,
  });

  const handleUpload = async (file: File) => {
    try {
      setFile(file);

      // Need to use fetch directly because the SDK doesn't support file uploads
      fetch(`http://localhost:8080/rpc/assets.uploadOpenAPIv3`, {
        method: "POST",
        headers: {
          "content-type": file.type,
          "content-length": file.size.toString(),
          "gram-session": session.session,
          "gram-project": project.projectSlug,
        },
        body: file,
      }).then(async (response) => {
        if (!response.ok) {
          throw new Error(`Upload failed`);
        }

        const result: UploadOpenAPIv3Result = await response.json();
        console.log("Upload successful:", result);

        setAsset(result);
        setApiName(file?.name.split(".")[0] ?? "My API");
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

    const deployment =
      await client.deployments.deploymentsNumberCreateDeployment({
        idempotencyKey: uuidv4(),
        createDeploymentRequestBody: {
          openapiv3Assets: [
            {
              assetId: asset.asset.id,
              name: apiName,
              slug: apiName.replace(" ", "-").toLowerCase(),
            },
          ],
        },
      });

    console.log("Deployment created:", deployment);
    setDeployment(deployment.deployment);
    refetchTools();
    setCreatingDeployment(false);
  };

  const numTools = tools?.tools.length ?? 0;

  const steps: StepProps[] = [
    {
      heading: "Upload OpenAPI Specification",
      description: "Upload your OpenAPI specification to get started.",
      display: <FileUpload onUpload={handleUpload} />,
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
        <Stack direction={"horizontal"} gap={2}>
          <Input
            value={apiName}
            onChange={(e) => setApiName(e.target.value)}
            placeholder="My API"
          />
          <Button onClick={createDeployment} disabled={!apiName}>
            Continue
          </Button>
        </Stack>
      ),
      displayComplete: <Type>✓ Source named "{apiName}"</Type>,
      isComplete: creatingDeployment || !!deployment,
    },
    {
      heading: "Generate Tools",
      description: "Gram will generate tools for your API.",
      display: numTools ? (
        <Type>✓ Created {tools?.tools.length} tools</Type>
      ) : (
        <Type>
          Gram is generating tools for your API. This may take a few seconds.
        </Type>
      ),
      displayComplete: <Type>✓ Created {tools?.tools.length} tools</Type>,
      isComplete: !!deployment,
    },
  ];

  return (
    <Page.Body>
      <Stepper
        steps={steps}
        onComplete={() => onOnboardingComplete?.(deployment!)}
      />
    </Page.Body>
  );
}
