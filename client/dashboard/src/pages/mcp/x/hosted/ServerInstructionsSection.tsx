import {
  useMcpMetadataMetadataForm,
  type UseMcpMetadataMetadataFormResult,
} from "@/components/mcp_install_page/useMcpMetadataForm";
import { Textarea } from "@/components/moon/textarea";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { useRBAC } from "@/hooks/useRBAC";
import { DEFAULT_MODEL } from "@/lib/models";
import { cn } from "@/lib/utils";
import { useModel } from "@/pages/playground/Openrouter";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { generateText } from "ai";
import React, { useState } from "react";
import { toast } from "sonner";

const INSTRUCTIONS_SOFT_LIMIT = 2000;

/** Server instructions for any MCP server, with AI drafting from its tools. */
export function ServerInstructionsSection({
  mcpServerId,
  name,
  tools,
}: {
  mcpServerId: string;
  name: string;
  tools: { name: string; description?: string | undefined }[];
}): React.JSX.Element {
  const metadataResult = useGetMcpMetadata({ mcpServerId }, undefined, {
    retry: (failureCount, err) =>
      !(err instanceof GramError && err.statusCode === 404) && failureCount < 3,
    throwOnError: false,
  });
  const form = useMcpMetadataMetadataForm(
    { kind: "mcp_server", mcpServerId },
    metadataResult.data?.metadata,
  );
  const metadataIs404 =
    metadataResult.error instanceof GramError &&
    metadataResult.error.statusCode === 404;
  // Saving is a full-record upsert, so wait for the record unless none exists yet.
  const isLoading =
    metadataResult.isLoading ||
    (metadataResult.isError && !metadataIs404) ||
    form.isLoading;
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const charCount = form.instructionsHandlers.value?.length ?? 0;
  const overLimit = charCount > INSTRUCTIONS_SOFT_LIMIT;

  return (
    <Stack gap={3}>
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
      {canWrite && (
        <Stack direction="horizontal" gap={2} justify="end">
          <GenerateInstructionsButton name={name} tools={tools} form={form} />
          <Button
            onClick={() => {
              void (async () => {
                try {
                  await form.saveAsync();
                  toast.success("Server instructions saved.");
                } catch {
                  toast.error("Failed to save instructions.");
                }
              })();
            }}
            disabled={isLoading || !form.instructionsDirty}
            size="sm"
          >
            <Button.Text>Save</Button.Text>
          </Button>
        </Stack>
      )}
    </Stack>
  );
}

function GenerateInstructionsButton({
  name,
  tools,
  form,
}: {
  name: string;
  tools: { name: string; description?: string | undefined }[];
  form: UseMcpMetadataMetadataFormResult;
}) {
  const [generating, setGenerating] = useState(false);
  const model = useModel(DEFAULT_MODEL);

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
${JSON.stringify({ name, tools: tools.map((t) => ({ name: t.name, description: t.description })) }, null, 2)}

Respond with ONLY the server instructions as plain text. Do not wrap in JSON or code fences.`,
      });

      // Populate the textarea via a synthetic change event
      const syntheticEvent = {
        target: { value: res.text.trim() },
      } as React.ChangeEvent<HTMLTextAreaElement>;
      form.instructionsHandlers.onChange(syntheticEvent);
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
      size="sm"
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
