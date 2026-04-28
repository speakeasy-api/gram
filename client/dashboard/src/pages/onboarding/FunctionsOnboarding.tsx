import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { GettingStartedInstructions } from "@/components/functions/GettingStartedInstructions";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";
import { CodeIcon } from "lucide-react";

export default function FunctionsOnboarding() {
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
                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-emerald-500/10">
                  <CodeIcon className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
                </div>
                <Heading variant="h3">Add Custom Functions</Heading>
              </Stack>
              <Type muted>
                Create custom tools using TypeScript functions. Functions let
                you extend your MCP server with custom logic and integrations.
              </Type>
            </Stack>

            {/* Instructions */}
            <GettingStartedInstructions />

            {/* Help text */}
            <Type small muted className="mt-6">
              Need help?{" "}
              <a
                href="https://www.speakeasy.com/docs/gram/getting-started/typescript"
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary hover:underline"
              >
                View the documentation
              </a>{" "}
              for detailed guides and examples.
            </Type>
          </div>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
