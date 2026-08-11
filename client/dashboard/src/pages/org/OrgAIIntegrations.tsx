import { SettingsPage } from "@/components/page-templates";
import { AIIntegrationConnectionRow } from "@/pages/org/ai-integration-connection-row";
import { AI_INTEGRATION_PROVIDERS } from "@/pages/org/ai-integration-providers";
import { LiteLLMIntegrationRow } from "@/pages/org/litellm-integration-row";
import { useRBAC } from "@/hooks/useRBAC";

// AI Integrations: one row per provider connection that expands to reveal its
// event and metric streams, each with its own status and pause toggle.
export default function OrgAIIntegrations(): JSX.Element {
  return (
    <SettingsPage
      scope={["org:read", "org:admin"]}
      title="AI Integrations"
      description="Connect AI providers and control the event and metric streams they import. Streams can be paused and resumed independently of the connection."
    >
      <OrgAIIntegrationsInner />
    </SettingsPage>
  );
}

export function OrgAIIntegrationsInner(): JSX.Element {
  const { hasScope } = useRBAC();

  return (
    <div className="border-border bg-card divide-border divide-y overflow-hidden border">
      {hasScope("org:admin") ? <LiteLLMIntegrationRow /> : null}
      {AI_INTEGRATION_PROVIDERS.map((provider) => (
        <AIIntegrationConnectionRow
          key={provider.provider}
          provider={provider}
        />
      ))}
    </div>
  );
}
