import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { AIIntegrationConnectionRow } from "@/pages/org/ai-integration-connection-row";
import { AI_INTEGRATION_PROVIDERS } from "@/pages/org/ai-integration-providers";

// AI Integrations: one row per provider connection that expands to reveal its
// event and metric streams, each with its own status and pause toggle.
// Rendered as a section of the Data Configuration page — provider imports are
// a data-inflow setting.
export function AIIntegrationsSection(): JSX.Element {
  return (
    <div>
      <Heading variant="h4" className="mb-2">
        AI Integrations
      </Heading>
      <Type muted small className="mb-6 block">
        Connect AI providers and control the event and metric streams they
        import. Streams can be paused and resumed independently of the
        connection.
      </Type>
      <div className="border-border bg-card divide-border divide-y overflow-hidden rounded-lg border">
        {AI_INTEGRATION_PROVIDERS.map((provider) => (
          <AIIntegrationConnectionRow
            key={provider.provider}
            provider={provider}
          />
        ))}
      </div>
    </div>
  );
}
