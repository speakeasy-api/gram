import { EditableText } from "@/components/editable-text";
import { CopyableSlug } from "@/components/name-and-slug";
import {
  ResourcesBadge,
  ToolCollectionBadge,
  ToolsetPromptsBadge,
} from "@/components/tool-collection-badge";
import { Badge } from "@/components/ui/badge";
import { Heading } from "@/components/ui/heading";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { useToolset } from "@/hooks/toolTypes";
import { Toolset } from "@/lib/toolTypes";
import { useSetIterationModeMutation } from "@gram/client/react-query/index.js";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useCallback, useState } from "react";
import { toast } from "sonner";

export const ToolsetHeader = ({
  toolsetSlug,
  actions,
}: {
  toolsetSlug: string;
  actions?: React.ReactNode;
}) => {
  const client = useSdkClient();
  const { data: toolset, refetch } = useToolset(toolsetSlug);
  const [isPopoverOpen, setIsPopoverOpen] = useState(false);

  const setIterationModeMutation = useSetIterationModeMutation({
    onSuccess: () => {
      refetch?.();
      setIsPopoverOpen(false);
    },
    onError: (error) => {
      toast.error(`Failed to update iteration mode: ${error.message}`);
    },
  });

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

  const handleIterationModeToggle = (enabled: boolean) => {
    // Cannot disable iteration mode while draft changes exist
    if (!enabled && toolset?.hasDraftChanges) {
      toast.error(
        "Cannot disable staging mode while draft changes exist. Promote or discard changes first.",
      );
      return;
    }

    setIterationModeMutation.mutate({
      request: {
        slug: toolsetSlug,
        setIterationModeRequestBody: {
          iterationMode: enabled,
        },
      },
    });
  };

  const isIterationMode = toolset?.iterationMode ?? false;
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
        <Stack direction="horizontal" gap={2} align="center">
          <Popover open={isPopoverOpen} onOpenChange={setIsPopoverOpen}>
            <PopoverTrigger asChild>
              <Button variant="secondary" size="sm">
                <Button.LeftIcon>
                  <Icon
                    name={isIterationMode ? "git-branch" : "zap"}
                    className="h-4 w-4"
                  />
                </Button.LeftIcon>
                <Button.Text>
                  {isIterationMode ? "Staging Mode" : "Iteration Mode"}
                </Button.Text>
              </Button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-80">
              <div className="flex flex-col gap-4">
                <div className="flex flex-col gap-1">
                  <Type variant="body" className="font-semibold">
                    Editing Mode
                  </Type>
                  <Type variant="body" className="text-muted-foreground text-sm">
                    {isIterationMode
                      ? "Changes are staged as drafts until you promote them to production."
                      : "Changes are applied immediately to your live MCP server."}
                  </Type>
                </div>
                <div className="flex items-center justify-between">
                  <div className="flex flex-col gap-0.5">
                    <Type variant="body" className="text-sm font-medium">
                      Staging Mode
                    </Type>
                    <Type variant="body" className="text-muted-foreground text-xs">
                      Stage changes before promoting
                    </Type>
                  </div>
                  <Switch
                    checked={isIterationMode}
                    onCheckedChange={handleIterationModeToggle}
                    disabled={setIterationModeMutation.isPending}
                    aria-label="Toggle staging mode"
                  />
                </div>
                {hasDraftChanges && (
                  <Type variant="body" className="text-muted-foreground text-xs">
                    You have unpublished changes. Promote or discard them to
                    disable staging mode.
                  </Type>
                )}
              </div>
            </PopoverContent>
          </Popover>
          {actions}
        </Stack>
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
