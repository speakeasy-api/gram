import { DeleteButton } from "@/components/delete-button";
import { EditableText } from "@/components/editable-text";
import { Page } from "@/components/page-layout";
import { ToolBadge } from "@/components/tool-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Heading } from "@/components/ui/heading";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { useGroupedTools } from "@/lib/toolNames";
import { capitalize, cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  HTTPToolDefinition,
  PromptTemplateKind,
  Toolset,
} from "@gram/client/models/components";
import {
  invalidateAllListToolsets,
  invalidateTemplate,
  useListToolsetsSuspense,
  useTemplateSuspense,
  useToolset,
  useUpdateTemplateMutation,
} from "@gram/client/react-query";
import { useListTools } from "@gram/client/react-query/listTools.js";
import { ResizablePanel, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Message } from "ai";
import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router";
import { v4 as uuidv4 } from "uuid";
import { ChatProvider, useChatContext } from "../playground/ChatContext";
import { ChatConfig, ChatWindow } from "../playground/ChatWindow";
import { ToolsetDropdown } from "../toolsets/ToolsetDropown";
import {
  Block,
  BlockInner,
  Input,
  instructionsPlaceholder,
  Step,
} from "./components";

export function ToolBuilderNew() {
  const newTemplate = {
    name: "new_composite_tool",
    description:
      "Do a series of steps using the tools in a toolset to accomplish a task",
    purpose:
      "Do a series of steps using the tools in a toolset to accomplish a task",
    inputs: [],
    steps: [],
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <ChatProvider>
          <ToolBuilder initial={newTemplate} />
        </ChatProvider>
      </Page.Body>
    </Page>
  );
}

export function ToolBuilderPage() {
  const { toolName } = useParams();

  const { data: toolsets } = useListToolsetsSuspense();

  const toolset =
    toolsets?.toolsets.find((t) =>
      t.promptTemplates.some((pt) => pt.name === toolName)
    ) ?? undefined;

  console.log(toolset, toolsets);

  const { data: template } = useTemplateSuspense({
    name: toolName,
  });

  const parsed = parsePrompt(template.template.prompt);

  //   const parsed = parsePrompt(template.template.prompt);
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <ChatProvider>
          <ToolBuilder
            initial={{
              id: template.template.id,
              toolset,
              historyId: template.template.historyId,
              name: template.template.name,
              description: template.template.description ?? "",
              ...parsed,
            }}
          />
        </ChatProvider>
      </Page.Body>
    </Page>
  );
}

function ToolBuilder({
  initial,
}: {
  initial: {
    id?: string;
    historyId?: string;
    name: string;
    description: string;
    purpose: string;
    inputs: Input[];
    steps: Step[];
    toolset?: Toolset;
  };
}) {
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const chat = useChatContext();
  const routes = useRoutes();

  const [name, setName] = useState(initial.name);
  const [description, setDescription] = useState(initial.description);
  const [purpose, setPurpose] = useState(initial.purpose);
  const [inputs, setInputs] = useState<Input[]>(initial.inputs);
  const [toolsetFilter, setToolset] = useState<Toolset | undefined>(
    initial.toolset
  );

  const { data: tools } = useListTools();
  const { data: toolsetData } = useToolset(
    {
      slug: toolsetFilter?.slug ?? "",
    },
    undefined,
    {
      enabled: !!toolsetFilter?.slug,
    }
  );

  const parseInputs = (s: string): string[] => {
    const inputs = s.match(/(\{\{[^}]+\}\})/g);
    return inputs?.map((input) => input.slice(2, -2)) ?? [];
  };

  const setStep = (step: Step) => {
    step.inputs = parseInputs(step.instructions);

    setSteps((prev) => {
      const newSteps = [...prev];
      const index = newSteps.findIndex((s) => s.id === step.id);
      newSteps[index] = step;
      return newSteps;
    });
  };

  // Ensures that the canonical tool and update function is set for the step
  const makeStep = (step: Step) => ({
    ...step,
    update: (s: Step) => setStep(s),
    canonicalTool:
      step.canonicalTool ??
      tools?.tools.find((t) => t.name === step.tool)?.canonicalName ??
      step.tool,
  });

  const [steps, setSteps] = useState<Step[]>(initial.steps.map(makeStep));

  useEffect(() => {
    setName(initial.name);
    setDescription(initial.description);
    setPurpose(initial.purpose);
    setInputs(initial.inputs);
    setSteps(initial.steps.map(makeStep));
  }, [initial]);

  console.log(steps);

  const insertTool = (tool: { name: string; canonicalName: string }) => {
    if (steps.length >= 10) {
      return;
    }
    const newSteps = [...steps];
    newSteps.push({
      id: uuidv4(),
      tool: tool.name,
      canonicalTool: tool.canonicalName,
      instructions: instructionsPlaceholder,
      update: (step) => setStep(step),
    });
    setSteps(newSteps);
  };

  const chatConfigRef: ChatConfig = useRef({
    toolsetSlug: toolsetFilter?.slug ?? null,
    environmentSlug: toolsetFilter?.defaultEnvironmentSlug ?? null,
    isOnboarding: false,
  });

  useEffect(() => {
    if (toolsetFilter?.slug) {
      chatConfigRef.current.toolsetSlug = toolsetFilter.slug;
    }
  }, [toolsetFilter]);

  useEffect(() => {
    if (toolsetData?.defaultEnvironmentSlug) {
      chatConfigRef.current.environmentSlug =
        toolsetData.defaultEnvironmentSlug;
    }
  }, [toolsetData]);

  const maybeFilteredTools = toolsetData?.httpTools ?? tools?.tools ?? [];

  // Merges inputs, preserving descriptions and preventing duplicates
  const mergeInputs = (newInputs: string[]) => {
    const currentInputs = inputs.map((input) => input.name);
    const toInsert = newInputs.filter(
      (input) => !currentInputs.includes(input)
    );
    setInputs([...inputs, ...toInsert.map((input) => ({ name: input }))]);
  };

  useEffect(() => {
    const inputsFromSteps = steps.flatMap((step) =>
      parseInputs(step.instructions)
    );
    mergeInputs(inputsFromSteps);
  }, [steps]);

  useEffect(() => {
    mergeInputs(parseInputs(purpose));
  }, [purpose]);

  const validateName = (v: string) => {
    if (v.length < 4) {
      return "Tool name must be at least 4 characters long";
    }
    if (v.length > 100) {
      return "Tool name must be less than 100 characters long";
    }
    if (tools?.tools.some((t) => t.name.toLowerCase() === v.toLowerCase())) {
      return "Tool name must be unique";
    }
    if (!v.match(/^[a-z]+(?:[a-z0-9_-]*[a-z0-9])?$/)) {
      return "Tool name must contain only lowercase letters, numbers, underscores, and hyphens";
    }
    return true;
  };

  const initialMessages: Message[] = [
    {
      id: "1",
      role: "assistant",
      content: "Use this chat to test out your tool!",
    },
  ];

  const AddStepButton = () => {
    const [open, setOpen] = useState(false);

    return (
      <ToolSelectPopover
        open={open}
        setOpen={setOpen}
        tools={maybeFilteredTools}
        onSelect={(tool) => {
          insertTool(tool);
          setOpen(false);
        }}
      >
        <Button
          variant="secondary"
          icon="plus"
          size="sm"
          className={
            "bg-card dark:bg-background border-stone-300 dark:border-stone-700 border-1"
          }
          disabled={steps.length >= 10}
        >
          Add Step
        </Button>
      </ToolSelectPopover>
    );
  };

  const tryNowButton = (
    <Button
      icon="play"
      size="sm"
      onClick={() =>
        chat.appendMessage({
          id: uuidv4(),
          role: "user",
          content: `\`\`\`xml\n${buildPrompt(purpose, inputs, steps)}\n\`\`\``,
        })
      }
    >
      Try Now
    </Button>
  );

  const anyChanges =
    description !== initial.description ||
    purpose !== initial.purpose ||
    inputs.length !== initial.inputs.length ||
    steps.length !== initial.steps.length ||
    inputs.some(
      (input) =>
        input.description !==
        initial.inputs.find((i) => i.name === input.name)?.description
    ) ||
    steps.some(
      (step) =>
        step.instructions !==
        initial.steps.find((s) => s.id === step.id)?.instructions
    );

  const revertButton = anyChanges && (
    <Button
      variant="ghost"
      size="sm"
      onClick={() => {
        setName(initial.name);
        setDescription(initial.description);
        setPurpose(initial.purpose);
        setInputs(initial.inputs);
        setSteps(initial.steps);
      }}
    >
      Revert
    </Button>
  );

  const { mutate: updatePrompt } = useUpdateTemplateMutation({
    onSettled: () => {
      invalidateTemplate(queryClient, [{ name }]);
    },
  });

  const saveButton = (
    <Button
      icon="save"
      disabled={!anyChanges}
      onClick={async () => {
        const argsJsonSchema = {
          type: "object",
          properties: Object.fromEntries(
            inputs.map((input) => [
              input.name,
              {
                type: "string",
                ...(input.description && {
                  description: input.description,
                }),
              },
            ])
          ),
          required: inputs.map((input) => input.name),
        };

        if (initial.id) {
          updatePrompt({
            request: {
              updatePromptTemplateForm: {
                id: initial.id,
                description,
                prompt: buildPrompt(purpose, inputs, steps),
                arguments: JSON.stringify(argsJsonSchema),
                toolsHint: steps.map((step) => step.canonicalTool),
              },
            },
          });
        } else {
          await client.templates.create({
            createPromptTemplateForm: {
              name,
              description,
              kind: PromptTemplateKind.HigherOrderTool,
              prompt: buildPrompt(purpose, inputs, steps),
              arguments: JSON.stringify(argsJsonSchema),
              toolsHint: steps.map((step) => step.canonicalTool),
              engine: "mustache",
            },
          });

          // Automatically add to the toolset
          await client.toolsets.updateBySlug({
            slug: toolsetFilter?.slug ?? "",
            updateToolsetRequestBody: {
              promptTemplateNames: [
                ...(toolsetData?.promptTemplates ?? []).map((t) => t.name),
                name,
              ],
            },
          });

          invalidateAllListToolsets(queryClient);
          routes.customTools.toolBuilder.goTo(name);
        }
      }}
    >
      Save
    </Button>
  );

  const toolName = initial.id ? (
    <Heading
      variant="h3"
      className={cn("normal-case w-fit", initial.id && "text-muted-foreground")}
      tooltip="Can't change name after tool is created"
    >
      {name}
    </Heading>
  ) : (
    <EditableText
      label="Tool Name"
      description="Give your tool a name. This influences tool selection."
      value={name}
      onSubmit={setName}
      validate={validateName}
      disabled={!!initial.id}
    >
      <Heading variant="h3" className="normal-case">
        {name}
      </Heading>
    </EditableText>
  );

  return (
    <ResizablePanel
      direction="horizontal"
      className="h-full [&>[role='separator']]:border-border [&>[role='separator']]:mx-8 [&>[role='separator']]:border-1"
    >
      <ResizablePanel.Pane minSize={35}>
        <Stack gap={1}>
          <Stack direction="horizontal" align="center" className="w-full">
            <Block label="Tool name" className="w-2/3">
              <BlockInner>{toolName}</BlockInner>
            </Block>
            <Block label="Toolset" className="w-1/3">
              <BlockInner>
                <ToolsetDropdown
                  selectedToolset={toolsetFilter}
                  setSelectedToolset={setToolset}
                  placeholder="Any"
                  noLabel
                  defaultSelection="most-recent"
                  disabledMessage={
                    steps.length > 0
                      ? "Can't change toolset after steps are added"
                      : undefined
                  }
                />
              </BlockInner>
            </Block>
          </Stack>
          <Block label="Description">
            <BlockInner>
              <EditableText
                label="Tool Description"
                description="Describe when and how this tool should be used. This field is how the LLM selects between tools."
                value={description}
                onSubmit={setDescription}
                lines={4}
              >
                <Type variant="subheading">{description}</Type>
              </EditableText>
            </BlockInner>
          </Block>
          <Block label="Purpose">
            <BlockInner>
              <EditableText
                label="Purpose"
                description="Describe what this tool should do when invoked"
                value={purpose}
                onSubmit={setPurpose}
                lines={4}
              >
                <Type variant="subheading">{purpose}</Type>
              </EditableText>
            </BlockInner>
          </Block>
          <Block label="Inputs">
            <BlockInner>
              <div className="flex flex-wrap gap-2">
                {inputs.length === 0 && (
                  <Type
                    muted
                    italic
                  >{`Inputs will appear here. Use {{braces}} in step instructions to create or reference them.`}</Type>
                )}
                {inputs.map((input) => (
                  <EditableText
                    key={input.name}
                    label={`{{${input.name}}} Description`}
                    description={"Describe what this input is for"}
                    value={input.description}
                    placeholder="A short description of the input"
                    onSubmit={(description) => {
                      setInputs(
                        inputs.map((i) =>
                          i.name === input.name ? { ...i, description } : i
                        )
                      );
                    }}
                  >
                    <Stack
                      direction="horizontal"
                      align="center"
                      className={inputStyles}
                      gap={1}
                    >
                      <span>
                        {input.description ? input.name + ":" : input.name}
                      </span>
                      {input.description && (
                        <span className="text-blue-400">...</span>
                      )}
                    </Stack>
                  </EditableText>
                ))}
              </div>
            </BlockInner>
          </Block>
          <Block label="Steps" labelRHS={`${steps.length} / 10`}>
            <Stack direction="vertical">
              {steps.map((step, index) => (
                <div key={index}>
                  <StepCard
                    step={step}
                    tools={maybeFilteredTools}
                    removeStep={() =>
                      setSteps(steps.filter((s) => s.id !== step.id))
                    }
                  />
                  <StepSeparator />
                </div>
              ))}
              <AddStepButton />
            </Stack>
          </Block>
          <Stack
            direction="horizontal"
            align="center"
            justify="end"
            gap={1}
            className="mt-4"
          >
            {revertButton}
            {saveButton}
          </Stack>
        </Stack>
      </ResizablePanel.Pane>
      <ResizablePanel.Pane minSize={35}>
        <ChatWindow
          configRef={chatConfigRef}
          additionalActions={tryNowButton}
          initialMessages={initialMessages}
        />
      </ResizablePanel.Pane>
    </ResizablePanel>
  );
}

const blockBackground = "bg-stone-100 dark:bg-stone-900";

const inputStyles =
  "bg-blue-100 dark:bg-blue-900 text-blue-900 dark:text-blue-100 px-1 rounded";
const SyntaxHighlight = ({ children }: { children: React.ReactNode }) => {
  if (typeof children !== "string") return <>{children}</>;

  // Split by curly braces
  const parts = children.split(/(\{\{[^}]+\}\})/g);
  return (
    <>
      {parts.map((part, i) => {
        if (part.match(/^\{\{.*\}\}$/)) {
          // Remove the curly braces and highlight the content
          const content = part.slice(2, -2);
          return (
            <span key={i} className={inputStyles}>
              {content}
            </span>
          );
        }
        return <span key={i}>{part}</span>;
      })}
    </>
  );
};

const StepCard = ({
  step,
  tools,
  removeStep,
}: {
  step: Step;
  tools: HTTPToolDefinition[];
  removeStep: (step: Step) => void;
}) => {
  const [open, setOpen] = useState(false);
  const tool = tools.find((t) => t.name === step.tool);

  const toolBadge = tool ? (
    <ToolBadge tool={tool} />
  ) : (
    <ToolSelectPopover
      open={open}
      setOpen={setOpen}
      tools={tools}
      onSelect={(tool) => {
        setOpen(false);
        step.update({ ...step, tool: tool.name });
      }}
    >
      <Badge
        variant="warning"
        tooltip={"Tool not found in project. Click to select a valid tool"}
        className="cursor-pointer"
      >
        {step.tool}
      </Badge>
    </ToolSelectPopover>
  );

  return (
    <BlockInner className="p-0 rounded-md overflow-clip">
      <Stack>
        <Stack
          direction="horizontal"
          align="center"
          justify="space-between"
          className="px-4 py-3 border-b border-stone-300 dark:border-stone-700"
        >
          <Type variant="subheading">Use the {toolBadge} tool to...</Type>
          <DeleteButton
            size="sm"
            tooltip="Delete step"
            onClick={() => removeStep(step)}
            className="mr-[-8px] mt-[-8px]"
          />
        </Stack>
        <div className={cn("px-4 py-3", blockBackground)}>
          <EditableText
            label="Instructions"
            description="Describe what this step should do. Use {{curly braces}} to declare inputs"
            value={step.instructions}
            lines={3}
            onSubmit={(instructions) => {
              step.update({ ...step, instructions });
            }}
          >
            <Type
              small
              className={cn(
                step.instructions === instructionsPlaceholder &&
                  "italic text-muted-foreground!"
              )}
            >
              <SyntaxHighlight>{step.instructions}</SyntaxHighlight>
            </Type>
          </EditableText>
        </div>
      </Stack>
    </BlockInner>
  );
};

const ToolSelectPopover = ({
  open,
  setOpen,
  onSelect,
  tools,
  children,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  onSelect: (tool: HTTPToolDefinition) => void;
  tools: HTTPToolDefinition[];
  children: React.ReactNode;
}) => {
  const groupedTools = useGroupedTools(tools);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent className="w-[200px] p-0">
        <Command>
          <CommandInput placeholder="Search..." className="h-9" />
          <CommandList>
            <CommandEmpty>No items found.</CommandEmpty>
            {groupedTools.map((group) => (
              <CommandGroup key={group.key} heading={capitalize(group.key)}>
                {group.tools.map((tool) => (
                  <CommandItem
                    key={tool.name}
                    value={tool.name}
                    className="cursor-pointer truncate"
                    onSelect={() => onSelect(tool)}
                  >
                    {tool.displayName}
                  </CommandItem>
                ))}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
};

const StepSeparator = () => {
  return (
    <div className="h-4 w-1/2 border-r-2 border-dashed border-stone-400 dark:border-stone-600" />
  );
};

const buildPrompt = (purpose: string, inputs: Input[], steps: Step[]) => {
  const inputsPortion = inputs
    .map(
      (input) =>
        `  <Input name="${input.name}" description="${input.description}" />`
    )
    .join("\n");

  const stepsPortion = steps
    .map((step) => {
      let stepInputs = step.inputs
        ?.map((input) => `<Input name="${input}" />`)
        .join("\n");
      if (stepInputs) {
        stepInputs = `\n    ${stepInputs}`;
      }

      return `  <CallTool tool_name="${step.tool}">
    <Instruction>${step.instructions.trim()}</Instruction>${stepInputs}
  </CallTool>`;
    })
    .join("\n");

  return `
<Purpose>
  <Instruction>
    You will be provided with a <Purpose>, a list of <Inputs>, and a <Plan>. Your goal is to use the <Plan> and <Inputs> to complete the <Purpose>.
  </Instruction>
  <Purpose>
    ${purpose}
  </Purpose>
</Purpose>
<Inputs>
  <Instruction>
    Ask me for each of these inputs before proceeding with the <Plan> below.
    If there is existing context to fill them out then go with that and only ask me for what is missing.
    Before executing the plan ask me to confirm all the provided details.
  </Instruction>
  ${inputsPortion.trim() || "No inputs needed"}
</Inputs>
<Plan>
  ${stepsPortion.trim()}
</Plan>`;
};

const parsePrompt = (
  prompt: string
): { purpose: string; inputs: Input[]; steps: Step[] } => {
  // Remove markdown backticks and xml tag if present
  const cleanPrompt = prompt.replace(/```xml\n|\n```/g, "").trim();

  // Extract purpose
  const purposeMatch = cleanPrompt.match(
    /<Purpose>\s*<Instruction>.*?<\/Instruction>\s*<Purpose>\s*(.*?)\s*<\/Purpose>\s*<\/Purpose>/s
  );
  const purpose = purposeMatch?.[1]?.trim() || "";

  // Extract inputs
  const inputsSection = cleanPrompt.match(/<Inputs>.*?<\/Inputs>/s)?.[0] || "";
  const inputMatches = [
    ...inputsSection.matchAll(
      /<Input name="([^"]+)"(?:\s+description="([^"]*)")?\s*\/>/g
    ),
  ];
  const inputs: Input[] = [];

  for (const match of inputMatches) {
    const name = match[1];
    if (name) {
      inputs.push({
        name,
        ...(match[2] && { description: match[2] }),
      });
    }
  }

  // Extract steps
  const planSection = cleanPrompt.match(/<Plan>(.*?)<\/Plan>/s)?.[0] || "";
  const stepMatches = [
    ...planSection.matchAll(
      /<CallTool tool_name="([^"]+)">\s*<Instruction>(.*?)<\/Instruction>(.*?)<\/CallTool>/gs
    ),
  ];
  const steps: Step[] = [];

  for (const match of stepMatches) {
    const [, tool, instructions, inputSection] = match;
    if (tool && instructions) {
      const stepInputs = [
        ...(inputSection || "").matchAll(/<Input name="([^"]+)"\s*\/>/g),
      ]
        .map((m) => m[1])
        .filter((input): input is string => !!input);

      steps.push({
        id: uuidv4(),
        tool,
        canonicalTool: tool,
        instructions: instructions.trim(),
        inputs: stepInputs,
        update: () => {
          console.error("update not implemented");
        }, // This will be replaced by the component when used
      });
    }
  }

  return { purpose, inputs, steps };
};

// TODO: do something with this
// const AutoToolBuilder = ({
//   steps,
//   setStep,
//   setSteps,
//   purpose,
//   description,
//   maybeFilteredTools,
//   setInputs,
// }: {
//   steps: Step[];
//   setStep: (step: Step) => void;
//   setSteps: (steps: Step[]) => void;
//   purpose: string;
//   description: string;
//   maybeFilteredTools: HTTPToolDefinition[];
//   setInputs: (inputs: Input[]) => void;
// }) => {
//   const model = useMiniModel();

//   useEffect(() => {
//     if (steps.length > 0) {
//       return;
//     }

//     generateObject({
//       model,
//       prompt: `
//         You are a composite tool builder. You are given a purpose for a tool and a list of available tools.
//         A composite tool consists of a series of steps which invoke an underlying tool along with instructions for how to use the tool.

//         Given the provided purpose, suggest:
//         - A description of the tool which will be provided to future LLMs invoking this tool. It should distinguish when and how this tool should be used.
//         - A list of inputs needed to invoke the tool. Any inputs here must appear in at least one step's instructions inside {{mustaches}}. Anything in {{mustaches}} must appear in this list.
//         - A list of steps which will accomplish the purpose.

//         Pay attention to the input format of the tools to see if other tools are need first to produce the inputs.
//         For example, a tool that expects an ID might first require a lookup tool to retrieve the ID given fuzzy user input.

//         The purpose is: ${purpose}
//         The current description is: ${description}
//         The available tools are: ${JSON.stringify(
//           maybeFilteredTools.map((t) => {
//             return {
//               name: t.name,
//               description: t.description,
//               schema: t.schema,
//             };
//           })
//         )}
//         `,
//       schema: z.object({
//         description: z.string(),
//         inputs: z.array(
//           z.object({
//             name: z.string(),
//             description: z.string(),
//           })
//         ),
//         steps: z.array(
//           z.object({
//             tool: z.string(),
//             instructions: z.string(),
//           })
//         ),
//       }),
//     }).then((result) => {
//       setInputs(result.object.inputs);
//       setSteps(
//         result.object.steps.map((step) => {
//           return {
//             id: uuidv4(),
//             tool: step.tool,
//             instructions: step.instructions,
//             update: (step) => setStep(step),
//           };
//         })
//       );
//     });
//   }, [purpose]);
// };
