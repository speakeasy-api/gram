import { AIToolsTable } from "@/components/shadow-ai/AIToolsTable";
import { Page } from "@/components/page-layout";
import { ShadowAISection } from "./ShadowAI";

export default function ShadowAIHarnesses(): JSX.Element {
  return (
    <ShadowAISection activeTab="harnesses">
      <Page.Section>
        <Page.Section.Title area="">Harnesses</Page.Section.Title>
        <Page.Section.Description>
          Every agentic coding tool and AI IDE that enrolled devices have
          reported, each with the organization’s decision on whether it may
          reach Gram’s MCP gateway. Open a row to record or change that
          decision.
        </Page.Section.Description>
        <Page.Section.Body>
          <AIToolsTable category="harness" canDecide />
        </Page.Section.Body>
      </Page.Section>
    </ShadowAISection>
  );
}
