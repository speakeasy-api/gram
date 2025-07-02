import { CreateThingCard } from "@/components/create-thing-card";
import { DeleteButton } from "@/components/delete-button";
import { Cards } from "@/components/ui/card";
import { Toolset, PromptTemplate } from "@gram/client/models/components";
import { useUpdateToolsetMutation } from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { getToolsetPrompts, PromptTemplateCard, usePrompts } from "../prompts/Prompts";
import { PromptSelectPopover } from "../prompts/PromptSelectPopover";
import { useRoutes } from "@/routes";
import { Type } from "@/components/ui/type";

export function PromptsTabContent({
    toolset,
    updateToolsetMutation,
  }: {
    toolset: Toolset;
    updateToolsetMutation: ReturnType<typeof useUpdateToolsetMutation>;
  }) {
    const allPrompts = usePrompts();
    const toolsetPrompts = getToolsetPrompts(toolset);
    const [promptSelectPopoverOpen, setPromptSelectPopoverOpen] = useState(false);
    const routes = useRoutes();
  
    const addPromptToToolset = (prompt: PromptTemplate) => {
      const currentPromptNames =
        toolset?.promptTemplates.map((t) => t.name) ?? [];
      if (currentPromptNames.includes(prompt.name)) {
        return;
      }
  
      updateToolsetMutation.mutate({
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: {
            promptTemplateNames: [...currentPromptNames, prompt.name],
          },
        },
      });
    };
  
    const removePromptFromToolset = (promptName: string) => {
      const currentPromptNames =
        toolset?.promptTemplates.map((t) => t.name) ?? [];
      updateToolsetMutation.mutate({
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: {
            promptTemplateNames: currentPromptNames.filter(
              (name) => name !== promptName
            ),
          },
        },
      });
    };
  
    return (
      <>
        <Cards loading={!toolset}>
          {toolsetPrompts?.map((prompt) => (
            <PromptTemplateCard
              key={prompt.name}
              template={prompt}
              actions={
                <DeleteButton
                  size="sm"
                  tooltip="Remove prompt from this toolset"
                  onClick={() => removePromptFromToolset(prompt.name)}
                />
              }
            />
          ))}
        </Cards>
        <Stack
          gap={3}
          direction={"horizontal"}
          align={"center"}
          className="w-full max-w-4xl"
        >
          {allPrompts && allPrompts?.length > 0 && (
            <>
              <PromptSelectPopover
                open={promptSelectPopoverOpen}
                setOpen={setPromptSelectPopoverOpen}
                onSelect={(prompt) => addPromptToToolset(prompt)}
              >
                {/* For some reason the popover doesnt show up in the right place without this div */}
                <div className="w-full">
                  <CreateThingCard className="mb-0!">
                    + Add Prompt
                  </CreateThingCard>
                </div>
              </PromptSelectPopover>
              <Type muted>or</Type>
            </>
          )}
          <div className="w-full">
            <routes.prompts.newPrompt.Link>
              <CreateThingCard className="mb-0!">+ Create Prompt</CreateThingCard>
            </routes.prompts.newPrompt.Link>
          </div>
        </Stack>
      </>
    );
  }
  