import { Page } from "@/components/page-layout";
import { GuideLayout } from "@/components/layouts/guide-layout";
import { RequireScope } from "@/components/require-scope";
import { GettingStartedInstructions } from "@/components/functions/GettingStartedInstructions";
import { Type } from "@/components/ui/type";

export default function FunctionsOnboarding(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:write" level="page">
          <GuideLayout>
            <GuideLayout.Header
              title="Add Custom Functions"
              subtitle="Create custom tools using TypeScript functions. Functions let you extend your MCP server with custom logic and integrations."
            />
            <GuideLayout.Body>
              <GettingStartedInstructions />

              <Type small muted>
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
            </GuideLayout.Body>
          </GuideLayout>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
