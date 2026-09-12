import { AIToolsTable } from "@/components/shadow-ai/AIToolsTable";
import { Page } from "@/components/page-layout";
import { ShadowAISection } from "./ShadowAI";

export default function ShadowAIModels(): JSX.Element {
  return (
    <ShadowAISection activeTab="models">
      <Page.Section>
        <Page.Section.Title area="">Models</Page.Section.Title>
        <Page.Section.Description>
          Open models enrolled devices are running locally. These never speak
          MCP to Gram, so there is nothing for the gateway to allow or block —
          this tab is inventory.
        </Page.Section.Description>
        <Page.Section.Body>
          {/* canDecide is false regardless of scope: a decision on a
              model that never connects would be a decision with nothing
              behind it. */}
          <AIToolsTable category="local_model" canDecide={false} />
        </Page.Section.Body>
      </Page.Section>
    </ShadowAISection>
  );
}
