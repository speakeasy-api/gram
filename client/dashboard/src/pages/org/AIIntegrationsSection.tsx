import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { AIIntegrationProviderCard } from "@/pages/org/ai-integration-provider-card";
import { AI_INTEGRATION_PROVIDERS } from "@/pages/org/ai-integration-providers";
import { Stack } from "@/components/ui/stack";

export function AIIntegrationsSection(): JSX.Element {
  return (
    <Stack gap={4}>
      <div>
        <Stack direction="horizontal" gap={2} align="center" className="mb-2">
          <Heading variant="h4">AI Integrations</Heading>
          <ReleaseStageBadge stage="preview" />
        </Stack>
        <Type muted small>
          Connect AI providers for usage and cost reporting, with room for more
          use cases as providers expose more data.
        </Type>
      </div>

      {AI_INTEGRATION_PROVIDERS.map((provider) => (
        <AIIntegrationProviderCard
          key={provider.provider}
          provider={provider}
        />
      ))}
    </Stack>
  );
}
