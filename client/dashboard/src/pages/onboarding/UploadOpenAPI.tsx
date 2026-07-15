import { Page } from "@/components/page-layout";
import { GuideLayout } from "@/components/layouts/guide-layout";
import { RequireScope } from "@/components/require-scope";

import { Type } from "@/components/ui/type";
import DeployStep from "@/components/upload-asset/deploy-step";
import NameDeploymentStep from "@/components/upload-asset/name-deployment-step";
import UploadAssetStep from "@/components/upload-asset/step";
import UploadAssetStepper from "@/components/upload-asset/stepper";
import { useStepper } from "@/components/upload-asset/stepper/use-stepper";
import UploadFileStep from "@/components/upload-asset/upload-file-step";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { useDeploymentLogs } from "@gram/client/react-query/deploymentLogs.js";
import { Button } from "@/components/ui/button";
import { CodeSnippet } from "@/components/ui/code-snippet";
import { ArrowRightIcon, RefreshCcwIcon } from "lucide-react";

export default function UploadOpenAPI(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:write" level="page">
          <GuideLayout>
            <GuideLayout.Header
              title="Import OpenAPI Specification"
              subtitle="Upload your OpenAPI spec to automatically generate tools for every endpoint. Supports JSON and YAML formats."
            />
            <GuideLayout.Body>
              <UploadAssetStepper.Provider step={1}>
                <UploadAssetStepper.Frame>
                  <GuideLayout.Step
                    index={1}
                    title="Upload OpenAPI Specification"
                  >
                    <Type muted small className="mb-3">
                      Upload your OpenAPI specification to get started.
                    </Type>
                    <UploadAssetStep step={1}>
                      <UploadAssetStep.Content>
                        <UploadFileStep />
                      </UploadAssetStep.Content>
                    </UploadAssetStep>
                  </GuideLayout.Step>

                  <GuideLayout.Step index={2} title="Name Your API">
                    <Type muted small className="mb-3">
                      The tools generated will be scoped under this name.
                    </Type>
                    <UploadAssetStep step={2}>
                      <UploadAssetStep.Content>
                        <NameDeploymentStep />
                      </UploadAssetStep.Content>
                    </UploadAssetStep>
                  </GuideLayout.Step>

                  <GuideLayout.Step index={3} title="Generate Tools">
                    <Type muted small className="mb-3">
                      The platform will generate tools for your API.
                    </Type>
                    <UploadAssetStep step={3}>
                      <UploadAssetStep.Content>
                        <DeployStep />
                      </UploadAssetStep.Content>
                    </UploadAssetStep>
                  </GuideLayout.Step>
                </UploadAssetStepper.Frame>
              </UploadAssetStepper.Provider>

              <Type small muted>
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
            </GuideLayout.Body>

            <GuideLayout.Footer>
              <FooterActions />
            </GuideLayout.Footer>
          </GuideLayout>
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
}): JSX.Element | null {
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
