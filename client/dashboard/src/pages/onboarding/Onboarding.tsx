import { Page } from "@/components/page-layout";
import FileUpload from "@/components/ui/upload";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";
import { useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { UploadOpenAPIv3Result } from "@gram/sdk/models/components";
import { v4 as uuidv4 } from "uuid";

export default function Onboarding() {
  const project = useProject();
  const session = useSession();
  const client = useSdkClient();

  const handleUpload = async (file: File) => {
    try {
      // Need to use fetch directly because the SDK doesn't support file uploads
      const response = await fetch(
        `http://localhost:8080/rpc/assets.uploadOpenAPIv3`,
        {
          method: "POST",
          headers: {
            "content-type": file.type,
            "content-length": file.size.toString(),
            "gram-session": session.session,
            "gram-project": project.projectSlug,
          },
          body: file,
        }
      );

      if (!response.ok) {
        throw new Error(`Upload failed`);
      }

      const result: UploadOpenAPIv3Result = await response.json();
      console.log("Upload successful:", result);

      const deployment =
        await client.deployments.deploymentsNumberCreateDeployment({
          idempotencyKey: uuidv4(),
          createDeploymentRequestBody: {
            openapiv3Assets: [
              {
                assetId: result.asset.id,
                name: file.name,
                slug: "default", // TODO
              },
            ],
          },
        });

      console.log("Deployment created:", deployment);
    } catch (error) {
      console.error("Upload failed:", error);
    }
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Stack gap={2}>
          <Heading variant="h2">Get Started</Heading>
          <Type variant="subheading" muted>
            Upload your OpenAPI specification to get started. Gram will generate
            tools for your API.
          </Type>
        </Stack>
        <FileUpload onUpload={handleUpload} />
      </Page.Body>
    </Page>
  );
}
