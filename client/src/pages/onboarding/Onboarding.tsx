import { Page } from "@/components/page-layout";
import FileUpload from "@/components/ui/upload";
import { Heading } from "@/components/ui/heading";
import { Text } from "@/components/ui/text";
import { Stack } from "@speakeasy-api/moonshine";

export default function Onboarding() {
  return (
    <Page>
      <Page.Header title="Quick Start" />
      <Page.Body>
        <Stack gap={2}>
          <Heading variant="h2">Get Started</Heading>
          <Text variant="subheading" muted>
            Upload your OpenAPI specification to get started. Gram will generate
            tools for your API.
          </Text>
        </Stack>
        <FileUpload />
      </Page.Body>
    </Page>
  );
}
