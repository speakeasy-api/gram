import { AIToolsTable } from "@/components/shadow-ai/AIToolsTable";
import { Page } from "@/components/page-layout";
import { ShadowAISection } from "./ShadowAI";

export default function ShadowAIAssistants(): JSX.Element {
  return (
    <ShadowAISection activeTab="assistants">
      <Page.Section>
        <Page.Section.Title area="">Assistants</Page.Section.Title>
        <Page.Section.Description>
          General-purpose AI assistants and agents reported by enrolled devices.
          Like harnesses they speak MCP to Gram, so the organization’s decision
          on gateway access applies to them. Open a row to see who runs it; the
          row’s menu records or changes that decision.
        </Page.Section.Description>
        <Page.Section.Body>
          <AIToolsTable category="assistant" canDecide />
        </Page.Section.Body>
      </Page.Section>
    </ShadowAISection>
  );
}
