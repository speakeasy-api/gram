import { useMcpMetadataMetadataForm } from "@/components/mcp_install_page/useMcpMetadataForm";
import { Textarea } from "@/components/moon/textarea";
import { RequireScope } from "@/components/require-scope";
import { useRBAC } from "@/hooks/useRBAC";
import { DEFAULT_MODEL } from "@/lib/models";
import { Toolset } from "@/lib/toolTypes";
import { cn } from "@/lib/utils";
import { useModel } from "@/pages/playground/Openrouter";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { generateText } from "ai";
import { useState } from "react";
import { toast } from "sonner";
import { FooterSaveButton, SettingsSection } from "../SettingsSection";

const INSTRUCTIONS_SOFT_LIMIT = 2000;

/**
 * Server instructions returned to LLMs on connect. Stored on MCP metadata and
 * keyed by the mcp_server id; the backing toolset only supplies tool
 * definitions for AI generation.
 */
export function InstructionsSection({
  mcpServer,
  toolset,
}: {
  mcpServer: McpServer;
  toolset: Toolset | undefined;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");

  const metadataResult = useGetMcpMetadata(
    { mcpServerId: mcpServer.id },
    undefined,
    { retry: false, throwOnError: false },
  );
  const form = useMcpMetadataMetadataForm(
    { kind: "mcp_server", mcpServerId: mcpServer.id },
    metadataResult.data?.metadata,
  );
  const isLoading = metadataResult.isLoading || form.isLoading;

  const charCount = form.instructionsHandlers.value?.length ?? 0;
  const overLimit = charCount > INSTRUCTIONS_SOFT_LIMIT;

  const handleSave = async () => {
    try {
      await form.saveAsync();
      toast.success("Server instructions saved.");
    } catch {
      toast.error("Failed to save instructions.");
    }
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Server Instructions</SettingsSection.Title>
        <SettingsSection.Description>
          Instructions returned to LLMs when they connect to your MCP server.
          Describe how your tools work together, required workflows, and any
          constraints.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="relative">
            <Textarea
              placeholder={`Describe how your tools work together, required workflows,\nand any constraints (rate limits, auth requirements, etc.).\n\nKeep it concise — don't repeat individual tool descriptions.`}
              className="min-h-[150px] w-full"
              value={form.instructionsHandlers.value ?? ""}
              onChange={form.instructionsHandlers.onChange}
              disabled={!canWrite}
            />
            {charCount > 0 && (
              <span
                className={cn(
                  "absolute right-3 bottom-2 text-xs",
                  overLimit ? "text-destructive" : "text-muted-foreground",
                )}
              >
                {charCount.toLocaleString()} /{" "}
                {INSTRUCTIONS_SOFT_LIMIT.toLocaleString()}
              </span>
            )}
          </div>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Keep it short like a quick-reference card.
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope scope="mcp:write" level="component">
              <GenerateInstructionsButton
                serverName={mcpServer.name ?? toolset?.name ?? "MCP Server"}
                toolset={toolset}
                onGenerated={(text) => {
                  const syntheticEvent = {
                    target: { value: text },
                  } as React.ChangeEvent<HTMLTextAreaElement>;
                  form.instructionsHandlers.onChange(syntheticEvent);
                }}
              />
              <FooterSaveButton
                pending={form.isLoading}
                disabled={isLoading || !form.instructionsDirty}
                onClick={() => void handleSave()}
              />
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function GenerateInstructionsButton({
  serverName,
  toolset,
  onGenerated,
}: {
  serverName: string;
  toolset: Toolset | undefined;
  onGenerated: (text: string) => void;
}) {
  const [generating, setGenerating] = useState(false);
  const model = useModel(DEFAULT_MODEL);

  const tools = toolset?.tools ?? [];

  const handleGenerate = async () => {
    if (tools.length === 0) {
      return;
    }

    setGenerating(true);
    try {
      const res = await generateText({
        model,
        prompt: `Write server instructions for the MCP server described below. Server instructions are returned to LLMs when they connect — they serve as a "user manual" independent of individual tool descriptions.

Best practices:
DO: Focus on cross-feature relationships (how tools work together, required sequences), document operational patterns and workflows, be explicit about constraints and limitations, keep it short like a quick-reference card.
DO NOT: Duplicate individual tool descriptions, include marketing claims, try to change model personality, write lengthy prose.

Keep the total output under ${INSTRUCTIONS_SOFT_LIMIT} characters.

Server details:
${JSON.stringify({ name: serverName, tools: tools.map((t) => ({ name: t.name, description: t.description })) }, null, 2)}

Respond with ONLY the server instructions as plain text. Do not wrap in JSON or code fences.`,
      });

      onGenerated(res.text.trim());
    } catch (err) {
      console.error("Failed to generate instructions:", err);
      toast.error("Failed to generate instructions. Please try again.");
    } finally {
      setGenerating(false);
    }
  };

  return (
    <Button
      variant="secondary"
      size="md"
      onClick={() => void handleGenerate()}
      disabled={generating || tools.length === 0}
    >
      <Button.LeftIcon>
        <Icon name="wand-sparkles" className="h-4 w-4" />
      </Button.LeftIcon>
      <Button.Text>
        {generating ? "Generating..." : "Generate with AI"}
      </Button.Text>
    </Button>
  );
}
