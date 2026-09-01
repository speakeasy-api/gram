import { AIDetectionsTable } from "@/components/ai-discovery/AIDetectionsTable";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";

export default function ShadowAI(): JSX.Element {
  const pageTitle = "Shadow AI";

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs substitutions={{ ["shadow-ai"]: pageTitle }} />
      </Page.Header>
      <Page.Body fullHeight className="pb-8">
        <RequireScope scope="org:admin" level="page">
          <Page.Section>
            <Page.Section.Title>{pageTitle}</Page.Section.Title>
            <Page.Section.Description>
              Organization-wide inventory of AI coding tools and local model
              runtimes detected on enrolled devices by device-agent scans.
            </Page.Section.Description>
            <Page.Section.Body>
              <AIDetectionsTable />
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
