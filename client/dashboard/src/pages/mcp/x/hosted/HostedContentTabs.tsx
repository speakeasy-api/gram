import { useTelemetry } from "@/contexts/Telemetry";
import { useToolset } from "@/hooks/toolTypes";
import type { Toolset } from "@/lib/toolTypes";
import { PromptsTabContent } from "@/pages/toolsets/PromptsTab";
import { ResourcesTabContent } from "@/pages/toolsets/resources/ResourcesTab";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateToolsetMutation } from "@gram/client/react-query/updateToolset.js";
import { useQueryClient } from "@tanstack/react-query";
import type React from "react";

const CONTENT = {
  resources: ResourcesTabContent,
  prompts: PromptsTabContent,
};

/** A hosted server's Resources or Prompts tab, edited on its toolset. */
export function HostedContentTab({
  toolset,
  kind,
}: {
  toolset: Toolset;
  kind: keyof typeof CONTENT;
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

  const Content = CONTENT[kind];
  // No tab title: the detail page's header and tabs already name it.
  return (
    <Content
      toolset={fullToolset}
      updateToolsetMutation={updateToolsetMutation}
    />
  );
}
