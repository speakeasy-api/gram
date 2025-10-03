import { CreateThingCard } from "@/components/create-thing-card";
import { Cards } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { PromptTemplate } from "@gram/client/models/components";
import { useUpdateToolsetMutation } from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import {
  PromptTemplateCard,
  usePrompts,
} from "../prompts/Prompts";
import { PromptSelectPopover } from "../prompts/PromptSelectPopover";
import { filterPromptTools, Toolset } from "@/lib/toolTypes";

export function PromptsTabContent({
  toolset,
  updateToolsetMutation,
}: {
  toolset: Toolset;
  updateToolsetMutation: ReturnType<typeof useUpdateToolsetMutation>;
}) {
  const { prompts: allPrompts } = usePrompts();
  const toolsetPrompts = filterPromptTools(toolset.tools);
  const [promptSelectPopoverOpen, setPromptSelectPopoverOpen] = useState(false);
  const routes = useRoutes();

  const currentPromptNames = toolsetPrompts?.map((t) => t.name) ?? [];

  const addPromptToToolset = (prompt: PromptTemplate) => {
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
      <Cards isLoading={!toolset}>
        {toolsetPrompts?.map((prompt) => (
          <PromptTemplateCard
            key={prompt.name}
            template={prompt}
            onDelete={() => removePromptFromToolset(prompt.name)}
            deleteLabel="Remove from toolset"
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
