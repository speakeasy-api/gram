import { Type } from "@/components/ui/type";
import { EditableText } from "@/components/editable-text";
import { CopyableSlug } from "@/components/name-and-slug";
import { ToolsetPromptsBadge, ToolsetToolsBadge } from "@/components/tools-badge";
import { useSdkClient } from "@/contexts/Sdk";
import { Toolset } from "@gram/client/models/components";
import { useToolset } from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { ToolsetEnvironmentBadge } from "./ToolsetEnvironmentBadge";
import { Heading } from "@/components/ui/heading";

export const ToolsetHeader = ({
    toolsetSlug,
    actions,
  }: {
    toolsetSlug: string;
    actions?: React.ReactNode;
  }) => {
    const client = useSdkClient();
    const { data: toolset, refetch } = useToolset({
      slug: toolsetSlug,
    });
  
    const updateToolset = async (changes: Partial<Toolset>) => {
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
    };
  
    return (
      <Stack gap={2} className="mb-4">
        <Stack direction="horizontal" justify="space-between" className="h-10">
          <CopyableSlug slug={toolset?.slug || ""} hidden={false}>
            <EditableText
              value={toolset?.name}
              onSubmit={(newValue) => updateToolset({ name: newValue })}
              label={"Toolset Name"}
              description={`Update the name of toolset '${toolset?.name}'`}
            >
              <Heading variant="h2">{toolset?.name}</Heading>
            </EditableText>
          </CopyableSlug>
          {actions}
        </Stack>
        <Stack direction="horizontal" gap={2} justify="space-between">
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
          <Stack direction="horizontal" gap={2}>
            <ToolsetPromptsBadge toolset={toolset} size="md" variant="outline" />
            <ToolsetToolsBadge toolset={toolset} size="md" variant="outline" />
            <ToolsetEnvironmentBadge
              toolset={toolset}
              size="md"
              variant="outline"
            />
          </Stack>
        </Stack>
      </Stack>
    );
  };