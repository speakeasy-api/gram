import { Page } from "@/components/page-layout";
import { useTelemetry } from "@/contexts/Telemetry";
import { useToolset } from "@/hooks/toolTypes";
import type { Toolset } from "@/lib/toolTypes";
import { PromptsTabContent } from "@/pages/toolsets/PromptsTab";
import { ResourcesTabContent } from "@/pages/toolsets/resources/ResourcesTab";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateToolsetMutation } from "@gram/client/react-query/updateToolset.js";
import { useQueryClient } from "@tanstack/react-query";
import type React from "react";

export function HostedResourcesTab({
  toolset,
}: {
  toolset: Toolset;
}): React.JSX.Element | null {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const { data: fullToolset } = useToolset(toolset.slug);

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", { action: "toolset_updated" });
      void invalidateAllToolset(queryClient);
    },
  });

  if (!fullToolset) return null;

  return (
    <Page.Section>
      <Page.Section.Title>Resources</Page.Section.Title>
      <Page.Section.Body>
        <ResourcesTabContent
          toolset={fullToolset}
          updateToolsetMutation={updateToolsetMutation}
        />
      </Page.Section.Body>
    </Page.Section>
  );
}

export function HostedPromptsTab({
  toolset,
}: {
  toolset: Toolset;
}): React.JSX.Element | null {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const { data: fullToolset } = useToolset(toolset.slug);

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", { action: "toolset_updated" });
      void invalidateAllToolset(queryClient);
    },
  });

  if (!fullToolset) return null;

  return (
    <Page.Section>
      <Page.Section.Title>Prompts</Page.Section.Title>
      <Page.Section.Body>
        <PromptsTabContent
          toolset={fullToolset}
          updateToolsetMutation={updateToolsetMutation}
        />
      </Page.Section.Body>
    </Page.Section>
  );
}
