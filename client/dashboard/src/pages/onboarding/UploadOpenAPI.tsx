import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Stepper, StepProps } from "@/components/stepper";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { Type } from "@/components/ui/type";
import DeployStep from "@/components/upload-asset/deploy-step";
import NameDeploymentStep from "@/components/upload-asset/name-deployment-step";
import UploadAssetStep from "@/components/upload-asset/step";
import UploadAssetStepper from "@/components/upload-asset/stepper";
import { useStepper } from "@/components/upload-asset/stepper/use-stepper";
import UploadFileStep from "@/components/upload-asset/upload-file-step";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { useDeploymentLogs } from "@gram/client/react-query/index.js";
import { Heading } from "@/components/ui/heading";
import { Button, CodeSnippet, Stack } from "@speakeasy-api/moonshine";
import { ArrowRightIcon, FileTextIcon, RefreshCcwIcon } from "lucide-react";

export default function UploadOpenAPI() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:write" level="page">
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
