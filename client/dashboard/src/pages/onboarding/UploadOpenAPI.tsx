import { FormPage } from "@/components/page-templates";
import { NEW_VERSION_SLUG_PARAM } from "@/components/sources/source-list-actions";
import { useProjectSources } from "@/components/sources/source-list";
import { Alert } from "@/components/ui/Alert";
import { Stack } from "@/components/ui/Stack";

import { Text } from "@/components/ui/Text";
import DeployStep from "@/components/upload-asset/deploy-step";
import NameDeploymentStep from "@/components/upload-asset/name-deployment-step";
import UploadAssetStep from "@/components/upload-asset/step";
import UploadAssetStepper from "@/components/upload-asset/stepper";
import type { ExistingDocument } from "@/components/upload-asset/stepper/use-stepper";
import { useStepper } from "@/components/upload-asset/stepper/use-stepper";
import UploadFileStep from "@/components/upload-asset/upload-file-step";
import { useRoutes } from "@/routes";
import { Button } from "@/components/ui/Button";
import { useProject } from "@/contexts/Auth";
import { ArrowRightIcon, RefreshCcwIcon } from "lucide-react";
import { useSearchParams } from "react-router";

// The page's copy differs by whether it adds a document or replaces one.
function pageCopy(existing: ExistingDocument | null): {
  title: string;
  description: string;
  nameStep: { title: string; description: string };
} {
  if (existing) {
    return {
      title: `New version of ${existing.name}`,
      description:
        "Upload the document that replaces the current version. Tools are regenerated from it on the next deployment.",
      nameStep: {
        title: "Confirm the document",
        description: "The new version keeps the document's name and slug.",
      },
    };
  }
  return {
    title: "Import OpenAPI specification",
    description:
      "Upload your OpenAPI spec to automatically generate tools for every endpoint. Supports JSON and YAML formats.",
    nameStep: {
      title: "Name Your API",
      description: "The tools generated will be scoped under this name.",
    },
  };
}

export default function UploadOpenAPI(): JSX.Element {
  const project = useProject();
  const [searchParams] = useSearchParams();
  // Reached with a slug, the flow uploads a new version of that document
  // instead of adding one. The list's "Upload new version" action and the
  // source page both link here that way.
  const slug = searchParams.get(NEW_VERSION_SLUG_PARAM);
  const { sources, isLoading } = useProjectSources();
  const match = slug
    ? sources.find(
        (source) => source.kind === "openapi" && source.slug === slug,
      )
    : undefined;
  const existing: ExistingDocument | null =
    match?.slug !== undefined ? { name: match.name, slug: match.slug } : null;
  const copy = pageCopy(existing);

  return (
    <FormPage
      scope="project:write"
      resourceId={project.id}
      title={copy.title}
      description={copy.description}
    >
      <div>
        {slug && !isLoading && !existing && (
          <Alert variant="warning" dismissible={false} className="mb-6">
            No OpenAPI document with the slug "{slug}" is in the active
            deployment, so this upload adds a new document instead.
          </Alert>
        )}
        {/* The stepper reads the document once, when it mounts, so it waits
            for the lookup rather than starting without it. */}
        {(!slug || !isLoading) && (
          <UploadAssetStepper.Provider step={1} existingDocument={existing}>
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
                  title={copy.nameStep.title}
                  description={copy.nameStep.description}
                />
                <UploadAssetStep.Content>
                  <NameDeploymentStep />
                </UploadAssetStep.Content>
              </UploadAssetStep>

              <UploadAssetStep step={3}>
                <UploadAssetStep.Indicator />
                <UploadAssetStep.Header
                  title="Generate Tools"
                  description="The platform will generate tools for your API."
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
        )}

        {/* Help text */}
        <Text small muted className="mt-6">
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
        </Text>
      </div>
    </FormPage>
  );
}

function FooterActions() {
  const stepper = useStepper();
  const routes = useRoutes();

  const deploymentId = stepper.meta.current.deployment?.id;
  // A new version returns to the shelf it was started from; a new document
  // goes on to the servers it can now be built into.
  const continueTo = stepper.meta.current.existingDocument
    ? routes.mcp.sources
    : routes.mcp;

  switch (stepper.state) {
    case "idle":
      return null;
    case "completed":
      return (
        <Button variant="primary" onClick={() => continueTo.goTo()}>
          <Button.Text>Continue</Button.Text>
          <Button.RightIcon>
            <ArrowRightIcon className="size-4" />
          </Button.RightIcon>
        </Button>
      );
    case "error":
      if (!deploymentId) {
        // This should never happen, but just in case
        return (
          <Button variant="primary" onClick={stepper.reset}>
            <Button.LeftIcon>
              <RefreshCcwIcon className="size-4" />
            </Button.LeftIcon>
            <Button.Text>Try Again</Button.Text>
          </Button>
        );
      }

      return (
        <>
          <Button
            variant="primary"
            onClick={() => routes.deployments.deployment.goTo(deploymentId)}
          >
            <Button.Text>View Logs</Button.Text>
            <Button.RightIcon>
              <ArrowRightIcon className="size-4" />
            </Button.RightIcon>
          </Button>
        </>
      );
  }
}
