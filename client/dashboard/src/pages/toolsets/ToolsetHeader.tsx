import { EditableText } from "@/components/editable-text";
import { CopyableSlug } from "@/components/name-and-slug";
import {
  ResourcesBadge,
  ToolCollectionBadge,
  ToolsetPromptsBadge,
} from "@/components/tool-collection-badge";
import { Badge } from "@/components/ui/badge";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { useToolset } from "@/hooks/toolTypes";
import { Toolset } from "@/lib/toolTypes";
import { Stack } from "@speakeasy-api/moonshine";
import { useCallback } from "react";

export const ToolsetHeader = ({
  toolsetSlug,
  actions,
}: {
  toolsetSlug: string;
  actions?: React.ReactNode;
}) => {
  const client = useSdkClient();
  const { data: toolset, refetch } = useToolset(toolsetSlug);

  const updateToolset = useCallback(
    async (changes: Partial<Toolset>) => {
      if (!toolset) {
        return;
      }

      await client.toolsets.updateBySlug({
        slug: toolset.slug,
        updateToolsetRequestBody: {
          name: changes.name,
          description: changes.description,
        },
      });

      refetch?.();
    },
    [toolset, client, refetch],
  );

  const hasDraftChanges = toolset?.hasDraftChanges ?? false;

  return (
    <Stack gap={2} className="mb-4">
      <Stack direction="horizontal" justify="space-between" className="h-10">
        <Stack direction="horizontal" gap={2} align="center">
          <CopyableSlug slug={toolset?.slug || ""} hidden={false}>
            <EditableText
              value={toolset?.name}
              onSubmit={(newValue) => updateToolset({ name: newValue })}
              label={"Toolset Name"}
              description={`Update the name of toolset '${toolset?.name}'`}
            >
              <Heading variant="h2" className="normal-case">
                {toolset?.name}
              </Heading>
            </EditableText>
          </CopyableSlug>
          {hasDraftChanges && (
            <Badge variant="warning" tooltip="This toolset has unpublished draft changes">
              Draft
            </Badge>
          )}
        </Stack>
        {actions}
      </Stack>
      <div className="flex flex-col gap-4 @2xl:flex-row @2xl:justify-between @2xl:gap-6">
        <EditableText
          value={toolset?.description}
          onSubmit={(newValue) => updateToolset({ description: newValue })}
          label={"Toolset Description"}
          description={`Update the description of toolset '${toolset?.name}'`}
          validate={(value) =>
            value.length > 0 && value.length < 100
              ? true
              : "Description must contain fewer than 100 characters"
          }
        >
          <Type variant="body" className="text-muted-foreground">
            {toolset?.description}
          </Type>
        </EditableText>
        <Stack direction="horizontal" gap={2} align="center">
          <ToolCollectionBadge
            toolNames={toolset?.tools.map((t) => t.name) ?? []}
            variant="neutral"
          />
          <ResourcesBadge
            resourceUris={toolset?.resources?.map((r) => r.uri) ?? []}
            variant="neutral"
          />
          <ToolsetPromptsBadge toolset={toolset} variant="neutral" />
        </Stack>
      </div>
    </Stack>
  );
};
