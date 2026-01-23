import { AutoSummarizeBadge } from "@/components/auto-summarize-badge";
import { EditableText } from "@/components/editable-text";
import { HttpRoute } from "@/components/http-route";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/cardOld";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { TOOL_NAME_REGEX } from "@/lib/constants";
import { isPromptTool, Tool } from "@/lib/toolTypes";
import { cn } from "@/lib/utils";
import {
  Confirm,
  HTTPToolDefinition,
  UpsertGlobalToolVariationForm,
} from "@gram/client/models/components";
import { invalidateTemplate, useDeployment } from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";

export function ToolCard({
  tool,
  onUpdate,
}: {
  tool: Tool;
  onUpdate: () => void;
}) {
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const sourceName = useToolSourceName(tool);
  const telemetry = useTelemetry();

  const updateVariation = async (
    vals: Partial<UpsertGlobalToolVariationForm>,
  ) => {
    if (tool.type === "http") {
      await client.variations.upsertGlobal({
        upsertGlobalToolVariationForm: {
          ...tool.variation,
          confirm: tool.variation?.confirm as Confirm, // TODO: Should the server return the same type?
          ...vals,
          srcToolUrn: tool.toolUrn,
          srcToolName: tool.canonicalName,
        },
      });
    } else {
      await client.templates.update({
        updatePromptTemplateForm: {
          ...tool,
          ...vals,
        },
      });
      invalidateTemplate(queryClient, [{ name: tool.name }]);
    }

    telemetry.capture("toolset_event", {
      action: "tool_variation_updated",
      tool_urn: tool.toolUrn,
      tool_name: tool.name,
      overridden_fields: Object.keys(vals).join(", "),
    });

    onUpdate();
  };

  const autoSummarizeEnabled = tool.type === "http" && tool.summarizer;

  const prefixTrimmed = tool.name.startsWith(sourceName + "_");
  const toolNameDisplay = prefixTrimmed
    ? tool.name.split(sourceName + "_")[1]
    : tool.name;

  const header = (
    <Stack direction="horizontal" gap={2} align="center">
      <EditableText
        value={tool.name}
        validate={(value) => {
          if (!TOOL_NAME_REGEX.test(value)) {
            return "Tool name may only contain letters, numbers, and underscores";
          }
          return true;
        }}
        onSubmit={(newValue) => updateVariation({ name: newValue })}
        label={"Tool Name"}
        description={`Update the name of tool '${tool.name}'`}
        disabled={isPromptTool(tool)}
      >
        <Stack direction="horizontal" align="center">
          {prefixTrimmed && (
            <Heading variant="h4" className="normal-case text-foreground/50">
              {sourceName}_
            </Heading>
          )}
          <Heading variant="h4" className="normal-case">
            {toolNameDisplay}
          </Heading>
        </Stack>
      </EditableText>
      {autoSummarizeEnabled && <AutoSummarizeBadge />}
    </Stack>
  );

  let tags = (
    <>
      <Badge
        variant="neutral"
        className="text-sm capitalize"
        tooltip={
          <span>
            This tool is from your{" "}
            <span className="font-bold capitalize">'{sourceName}'</span> source
          </span>
        }
      >
        {sourceName}
      </Badge>
      {tool.type === "http" &&
        tool.tags.map((tag) => (
          <Badge key={tag} variant="neutral" className="text-sm capitalize">
            {tag}
          </Badge>
        ))}
      {isPromptTool(tool) && (
        <Badge
          variant="neutral"
          className="text-sm capitalize"
          tooltip={`Subtools: ${tool.toolsHint.join(", ")}`}
        >
          Custom Tool
        </Badge>
      )}
    </>
  );

  if (tool.type === "prompt") {
    tags = (
      <Badge
        variant="neutral"
        className="text-sm capitalize"
        tooltip={<span>This is a custom tool</span>}
      >
        Custom
      </Badge>
    );
  }

  // TODO: this should get much more granular (function asset name) as the UX gets built out
  if (tool.type === "function") {
    tags = (
      <Badge
        variant="neutral"
        className="text-sm capitalize"
        tooltip={<span>This is a function tool</span>}
      >
        Function
      </Badge>
    );
  }

  return (
    <Card size="sm" className="h-full">
      <Card.Header>
        <Card.Title>{header}</Card.Title>
        <Card.Info>{tags}</Card.Info>
        <Card.Description>
          {tool.type === "http" ? (
            <HttpRoute method={tool.httpMethod} path={tool.path} />
          ) : tool.type === "prompt" ? (
            <Type small mono muted>
              {tool.toolsHint.join(", ")}
            </Type>
          ) : null}
        </Card.Description>
      </Card.Header>
      <Card.Content className="h-full">
        <div className="border-l-2 pl-4 h-full">
          <EditableText
            value={tool.description}
            onSubmit={(newValue) => updateVariation({ description: newValue })}
            label={"Tool Description"}
            description={`Update the description of tool '${tool.name}'`}
            lines={3}
          >
            <Type
              className={cn(
                "line-clamp-3 text-muted-foreground wrap-anywhere!",
                !tool.description && "italic",
              )}
            >
              {tool.description || "No description provided"}
            </Type>
          </EditableText>
        </div>
      </Card.Content>
    </Card>
  );
}

function useToolSourceName(tool: Tool) {
  const { data: deployment } = useDeployment(
    {
      id: (tool as HTTPToolDefinition).deploymentId,
    },
    undefined,
    {
      enabled: tool.type === "http" && !tool.packageName,
    },
  );

  switch (tool.type) {
    case "http": {
      const openApiId = tool.openapiv3DocumentId;
      const assets = deployment?.openapiv3Assets;

      return assets?.find((asset) => asset.id === openApiId)?.slug;
    }
    case "function": {
      return "Function";
    }
    default: {
      return "Custom";
    }
  }
}
