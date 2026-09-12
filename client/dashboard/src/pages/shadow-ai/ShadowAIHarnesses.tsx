import { AIToolsTable } from "@/components/shadow-ai/AIToolsTable";
import { Page } from "@/components/page-layout";
import { useRBAC } from "@/hooks/useRBAC";
import { ShadowAISection } from "./ShadowAI";

export default function ShadowAIHarnesses(): JSX.Element {
  const { hasAnyScope } = useRBAC();
  const canDecide = hasAnyScope(["org:admin"]);

  return (
    <ShadowAISection activeTab="harnesses">
      <Page.Section>
        <Page.Section.Title area="">Harnesses</Page.Section.Title>
        <Page.Section.Description>
          Every agentic coding tool and AI IDE enrolled devices have reported,
          with the organization’s decision on whether it may reach Gram’s MCP
          gateway.
          {canDecide
            ? " Open a row to record or change that decision."
            : " Recording a decision needs organization administrator access."}
        </Page.Section.Description>
        <Page.Section.Body>
          <AIToolsTable category="harness" canDecide={canDecide} />
        </Page.Section.Body>
      </Page.Section>
    </ShadowAISection>
  );
}
