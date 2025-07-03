import { AutoSummarizeBadge } from "@/components/auto-summarize-badge";
import { DeleteButton } from "@/components/delete-button";
import { EditableText } from "@/components/editable-text";
import { HttpRoute } from "@/components/http-route";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { TOOL_NAME_REGEX } from "@/lib/constants";
import { cn } from "@/lib/utils";
import { UpsertGlobalToolVariationForm, Confirm, HTTPToolDefinition } from "@gram/client/models/components";
import { invalidateTemplate, useDeployment } from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { ToolDefinition } from "./types";
import { Heading } from "@/components/ui/heading";
import { Dot } from "@/components/ui/dot";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { Button } from "@/components/ui/button";

export function ToolCard({
    tool,
    onRemove,
    onUpdate,
  }: {
    tool: ToolDefinition;
    onRemove: () => void;
    onUpdate: () => void;
  }) {
    const queryClient = useQueryClient();
    const client = useSdkClient();
    const sourceName = useToolSourceName(tool);
    const telemetry = useTelemetry();
    const toolNameDisplay = sourceName
      ? tool.name.replace(sourceName + "_", "")
      : tool.name;
  
    const updateVariation = async (
      vals: Partial<UpsertGlobalToolVariationForm>
    ) => {
      if (tool.type === "http") {
        await client.variations.upsertGlobal({
          upsertGlobalToolVariationForm: {
            srcToolName: tool.name,
            ...tool.variation,
            confirm: tool.variation?.confirm as Confirm, // TODO: Should the server return the same type?
            ...vals,
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
        tool_name: tool.name,
        overridden_fields: Object.keys(vals).join(", "),
      });
  
      onUpdate();
    };
  
    const autoSummarizeEnabled = tool.type === "http" && tool.summarizer;
  
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
          disabled={tool.type === "prompt"}
        >
          <Stack direction="horizontal" gap={2} align="center" wrap="wrap">
            <Heading
              variant="h4"
              className="text-muted-foreground capitalize"
              tooltip={"This tool is from your " + sourceName + " source"}
            >
              {sourceName}
            </Heading>
            <Dot />
            <Heading variant="h4">{toolNameDisplay}</Heading>
          </Stack>
        </EditableText>
        {autoSummarizeEnabled && <AutoSummarizeBadge />}
      </Stack>
    );
  
    const tags = (
      <>
        {tool.tags.map((tag) => (
          <Badge key={tag} variant="secondary" className="text-sm capitalize">
            {tag}
          </Badge>
        ))}
      </>
    );
  
    const autoSummarizeButton = (
      <Button
        icon={autoSummarizeEnabled ? "check" : "sparkles"}
        variant="ghost"
        size="sm"
        tooltip="An experimental feature. Attempt to Auto-summarize the tool's response via separate LLM call using your account's LLM credentials and prevent large data from overwhelming the context window. This can only be used when authenticated to Gram."
        onClick={() => {
          updateVariation({
            summarizer: autoSummarizeEnabled ? undefined : "auto",
          });
        }}
      >
        {autoSummarizeEnabled ? "Auto-Summarize" : "Auto-Summarize (alpha)"}
      </Button>
    );
  
    return (
      <Card size="sm">
        <Card.Header>
          <Card.Title>{header}</Card.Title>
          <Card.Info>{tags}</Card.Info>
          <Card.Description>
            {tool.type === "http" ? (
              <HttpRoute method={tool.httpMethod} path={tool.path} />
            ) : (
              <Type small mono muted>
                {tool.toolsHint.join(", ")}
              </Type>
            )}
          </Card.Description>
          {/* Temoprarily for launch we have discussed not allowing new auto-summarization to be enabled while we discuss. The code will stay around for now. */}
          <Card.Actions>
            {tool.type === "http" && autoSummarizeEnabled && autoSummarizeButton}
            <DeleteButton
              size="sm"
              tooltip="Remove tool from this toolset"
              onClick={onRemove}
            />
          </Card.Actions>
        </Card.Header>
        <Card.Content>
          <div className="border-l-2 pl-4">
            <EditableText
              value={tool.description}
              onSubmit={(newValue) => updateVariation({ description: newValue })}
              label={"Tool Description"}
              description={`Update the description of tool '${tool.name}'`}
              lines={3}
            >
              <Type
                className={cn(
                  "line-clamp-3 text-muted-foreground",
                  !tool.description && "italic"
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
  
function useToolSourceName(tool: ToolDefinition) {
    const { data: deployment } = useDeployment(
      {
        id: (tool as HTTPToolDefinition).deploymentId,
      },
      undefined,
      {
        enabled: tool.type === "http" && !tool.packageName,
      }
    );
  
    if (tool.packageName) {
      return tool.packageName;
    }
  
    if (tool.type === "prompt") {
      return "Custom";
    }
  
    return deployment?.openapiv3Assets.find(
      (asset) => asset.id === tool.openapiv3DocumentId
    )?.slug;
  }
