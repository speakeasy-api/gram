import { Page } from "@/components/page-layout";
import FileUpload from "@/components/ui/upload";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";
import { useUploadOpenAPIv3Mutation } from "@gram/sdk/react-query";
import { useProject } from "@/contexts/Auth";

export default function Onboarding() {
  const project = useProject();
  const uploadOpenApi = useUploadOpenAPIv3Mutation();

  const handleUpload = (file: File) => {
    uploadOpenApi.mutate({
      request: { contentLength: file.size, gramProject: project.projectSlug },
    });
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
