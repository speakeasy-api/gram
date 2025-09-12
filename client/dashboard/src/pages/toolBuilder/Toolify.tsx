import { ToolsetToolsBadge } from "@/components/tools-badge";
import { Button } from "@speakeasy-api/moonshine";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Spinner } from "@/components/ui/spinner";
import { TextArea } from "@/components/ui/textarea";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { generateObject } from "ai";
import { createContext, useContext, useState } from "react";
import { z } from "zod";
import { useMiniModel } from "../playground/Openrouter";
import { ToolsetDropdown } from "../toolsets/ToolsetDropown";
import { useToolDefinitions } from "../toolsets/types";
import { useToolset } from "@gram/client/react-query";

const SuggestionSchema = z.object({
  name: z.string(),
  description: z.string(),
  inputs: z.array(
    z.object({
      name: z.string(),
      description: z.string(),
    })
  ),
  steps: z.array(
    z.object({
      tool: z.string(),
      instructions: z.string(),
    })
  ),
});

type ToolifyContextType = {
  toolset: ToolsetEntry;
  purpose: string;
  suggestion: z.infer<typeof SuggestionSchema>;
};

//eslint-disable-next-line @typescript-eslint/no-explicit-any
const emptyCtx: ToolifyContextType = {} as any;

const ToolifyContext = createContext<
  ToolifyContextType & { set: (t: ToolifyContextType) => void }
>({ ...emptyCtx, set: () => {} });

export const ToolifyProvider = ({
  children,
}: {
  children: React.ReactNode;
}) => {
  const [state, setState] = useState<ToolifyContextType>(emptyCtx);

  return (
    <ToolifyContext.Provider value={{ ...state, set: setState }}>
      {children}
    </ToolifyContext.Provider>
  );
};

export const useToolifyContext = () => {
  return useContext(ToolifyContext);
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
  const { data: toolset } = useToolset(
    { slug: selectedToolset?.slug ?? "" },
    undefined,
    { enabled: !!selectedToolset?.slug }
  );
  const tools = useToolDefinitions(toolset);

  const { set } = useToolifyContext();

  const model = useMiniModel();

  const onSubmit = async () => {
    if (!selectedToolset) {
      return;
    }

    setInProgress(true);

    const res = await generateObject({
      model,
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
          }
        ]
      }
  
      Requirements:
      - The inputs array should contain objects with name and description for each input parameter
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
        })
      )}
                  `,
      schema: SuggestionSchema,
    });

    set({
      toolset: selectedToolset,
      purpose,
      suggestion: res.object as z.infer<typeof SuggestionSchema>,
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
            <Heading variant="h5" className="normal-case font-medium">
              What tools does this higher-order tool need access to?
            </Heading>
            <Stack direction="horizontal" gap={2} align="center">
              <ToolsetDropdown
                selectedToolset={selectedToolset}
                setSelectedToolset={(toolset) => setSelectedToolset(toolset)}
              />
              <ToolsetToolsBadge toolset={selectedToolset} />
            </Stack>
          </Stack>
          <Stack gap={1}>
            <Heading variant="h5" className="normal-case font-medium">
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
            <Button variant="tertiary"
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
