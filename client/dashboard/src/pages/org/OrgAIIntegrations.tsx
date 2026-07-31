import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { AIIntegrationConnectionRow } from "@/pages/org/ai-integration-connection-row";
import { AI_INTEGRATION_PROVIDERS } from "@/pages/org/ai-integration-providers";

// AI Integrations: one row per provider connection that expands to reveal its
// event and metric streams, each with its own status and pause toggle.
export default function OrgAIIntegrations(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <OrgAIIntegrationsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function OrgAIIntegrationsInner() {
  return (
    <Page.Section>
      <Page.Section.Title>AI Integrations</Page.Section.Title>
      <Page.Section.Description>
        Connect AI providers and control the event and metric streams they
        import. Streams can be paused and resumed independently of the
        connection.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="border-border bg-card divide-border divide-y overflow-hidden rounded-lg border">
          {AI_INTEGRATION_PROVIDERS.map((provider) => (
            <AIIntegrationConnectionRow
              key={provider.provider}
              provider={provider}
            />
          ))}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
