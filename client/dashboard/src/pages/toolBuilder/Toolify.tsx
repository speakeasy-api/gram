import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Spinner } from "@/components/ui/spinner";
import { TextArea } from "@/components/ui/textarea";
import { useToolset } from "@/hooks/toolTypes";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { generateObject } from "ai";
import { useState } from "react";
import { z } from "zod";
import { useMiniModel } from "../playground/Openrouter";
import { ToolsetDropdown } from "../toolsets/ToolsetDropown";
import {
  SuggestionSchema,
  ToolifyContext,
  emptyCtx,
  useToolifyContext,
} from "./useToolifyContext";

export const ToolifyProvider = ({
  children,
}: {
  children: React.ReactNode;
}) => {
  const [state, setState] = useState(emptyCtx);

  return (
    <ToolifyContext.Provider value={{ ...state, set: setState }}>
      {children}
    </ToolifyContext.Provider>
  );
};

export const ToolifyDialog = ({
  open,
  setOpen,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
}) => {
  const routes = useRoutes();

  const [inProgress, setInProgress] = useState(false);
  const [purpose, setPurpose] = useState("");
  const [selectedToolset, setSelectedToolset] = useState<ToolsetEntry>();
  const { data: toolset } = useToolset(selectedToolset?.slug);

  const tools = toolset?.tools ?? [];

  const { set } = useToolifyContext();

  const model = useMiniModel();

  const onSubmit = async () => {
    if (!selectedToolset) {
      return;
    }

    setInProgress(true);

    const res = await generateObject({
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      model: model as any,
      mode: "json",
      prompt: `
      You are a composite tool builder. You are given a purpose for a tool and a list of available tools.
      A composite tool consists of a series of steps which invoke an underlying tool along with instructions for how to use the tool.
  
      Given the provided purpose, suggest a JSON response with the following structure:
      {
        "name": "Tool Name (fewer than 40 characters)",
        "description": "Description for future LLMs about when and how to use this tool",
        "inputs": [
          {
            "name": "input_name",
            "description": "What this input is for"
          }
        ],
        "steps": [
          {
            "tool": "exact_tool_name",
            "instructions": "How to use the tool with {{input_name}} placeholders"
          },
           {
            "tool": "different_tool_name",
            "instructions": "How to use the different tool with {{input_name}} placeholders"
          }
        ]
      }
  
      Requirements:
      - The inputs array should contain objects with name and description for each input parameter, unique by name
      - Any inputs must appear in at least one step's instructions inside {{mustaches}}
      - Each step should invoke exactly one tool by its provided name
      - Pay attention to tool schemas - you may need multiple steps if one tool's output feeds another's input
  
      The purpose is: ${purpose}

      The available tools are: ${JSON.stringify(
        tools.map((t) => {
          return {
            name: t.name,
            description: t.description,
            schema: t.schema,
          };
        }),
      )}
                  `,
      schema: SuggestionSchema,
    });

    const suggestion = res.object as z.infer<typeof SuggestionSchema>;
    const uniqueInputs = suggestion.inputs.filter(
      (input, index, self) =>
        index === self.findIndex((i) => i.name === input.name),
    );

    set({
      toolset: selectedToolset,
      purpose,
      suggestion: {
        ...suggestion,
        inputs: uniqueInputs,
      },
    });

    setInProgress(false);

    routes.customTools.toolBuilderNew.goTo();
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>
            <Stack direction="horizontal" gap={2} align="center">
              <Icon name="wand-sparkles" className="text-muted-foreground" />
              Toolify
            </Stack>
          </Dialog.Title>
          <Dialog.Description>
            Create a higher-order tool that chains together multiple tools
          </Dialog.Description>
        </Dialog.Header>
        <Stack gap={4}>
          <Stack gap={1}>
            <Heading variant="h5" className="font-medium normal-case">
              What tools does this higher-order tool need access to?
            </Heading>
            <Stack direction="horizontal" gap={2} align="center">
              <ToolsetDropdown
                selectedToolset={selectedToolset}
                setSelectedToolset={(toolset) => setSelectedToolset(toolset)}
              />
              <ToolCollectionBadge
                toolNames={toolset ? toolset.tools.map((t) => t.name) : []}
                warnOnTooManyTools
              />
            </Stack>
          </Stack>
          <Stack gap={1}>
            <Heading variant="h5" className="font-medium normal-case">
              What should this tool do?
            </Heading>
            <TextArea
              value={purpose}
              onChange={(value) => setPurpose(value)}
              disabled={inProgress}
              placeholder="What should the tool do?"
              rows={4}
            />
          </Stack>
        </Stack>
        <Dialog.Footer className="sm:justify-between">
          <Button variant="tertiary" onClick={() => setOpen(false)}>
            Back
          </Button>
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button
              variant="tertiary"
              onClick={() => routes.customTools.toolBuilderNew.goTo()}
            >
              Skip
            </Button>
            <Button onClick={onSubmit} disabled={!purpose || inProgress}>
              {inProgress && <Spinner />}
              {inProgress ? "Generating..." : "Toolify"}
            </Button>
          </div>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
};
