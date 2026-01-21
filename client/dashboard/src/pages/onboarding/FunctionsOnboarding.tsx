import { Page } from "@/components/page-layout";
import { GettingStartedInstructions } from "@/components/functions/GettingStartedInstructions";

export default function FunctionsOnboarding() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Add Custom Functions</Page.Section.Title>
          <Page.Section.Description>
            Create custom tools using TypeScript functions
          </Page.Section.Description>
          <Page.Section.Body>
            <div className="max-w-2xl">
              <GettingStartedInstructions />
            </div>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}
