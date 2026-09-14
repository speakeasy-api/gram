import { AIToolsTable } from "@/components/shadow-ai/AIToolsTable";
import { Page } from "@/components/page-layout";
import { useRBAC } from "@/hooks/useRBAC";
import { ShadowAISection } from "./ShadowAI";

export default function ShadowAIAssistants(): JSX.Element {
  const { hasAnyScope } = useRBAC();
  const canDecide = hasAnyScope(["org:admin"]);

  return (
    <ShadowAISection activeTab="assistants">
      <Page.Section>
        <Page.Section.Title area="">Assistants</Page.Section.Title>
        <Page.Section.Description>
          General-purpose AI assistants and agents enrolled devices have
          reported. Like harnesses they speak MCP to Gram, so the organization’s
          decision on gateway access applies to them.
          {canDecide
            ? " Open a row to record or change that decision."
            : " Recording a decision needs organization administrator access."}
        </Page.Section.Description>
        <Page.Section.Body>
          <AIToolsTable category="assistant" canDecide={canDecide} />
        </Page.Section.Body>
      </Page.Section>
    </ShadowAISection>
  );
}
