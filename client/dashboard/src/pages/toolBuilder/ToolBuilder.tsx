import { Block, BlockInner } from "@/components/block";
import { DeleteButton } from "@/components/delete-button";
import { EditableText } from "@/components/editable-text";
import { Page } from "@/components/page-layout";
import { ToolBadge } from "@/components/tool-badge";
import { Badge } from "@/components/ui/badge";
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
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useToolset } from "@/hooks/toolTypes";
import { MUSTACHE_VAR_REGEX, slugify, TOOL_NAME_REGEX } from "@/lib/constants";
import { handleAPIError } from "@/lib/errors";
import { Tool, useGroupedTools } from "@/lib/toolTypes";
import { capitalize, cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  PromptTemplateKind,
  ToolsetEntry,
} from "@gram/client/models/components";
import {
  invalidateAllListToolsets,
  invalidateAllTemplate,
  invalidateAllTemplates,
  useListToolsetsSuspense,
  useTemplateSuspense,
  useUpdateTemplateMutation,
} from "@gram/client/react-query";
import { Button, Icon, ResizablePanel, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import React, { useEffect, useRef, useState } from "react";
import { useParams } from "react-router";
import { toast } from "@/lib/toast";
import { v7 as uuidv7 } from "uuid";
import { EnvironmentDropdown } from "../environments/EnvironmentDropdown";
import { ChatProvider, useChatContext } from "../playground/ChatContext";
import { ChatConfig, ChatWindow } from "../playground/ChatWindow";
import { ToolsetDropdown } from "../toolsets/ToolsetDropown";
import { useToolifyContext } from "./Toolify";

type Input = {
  name: string;
  description?: string;
};

type Step = {
  id: string;
  tool?: string;
  canonicalTool?: string;
  toolUrn?: string;
  instructions: string;
  inputs?: string[];
  update: (step: Step) => void;
};

function higherOrderToolToJSON(tool: CustomTool): string {
  return JSON.stringify(tool, null, 2);
}

const instructionsPlaceholder =
  "Interpret what to do with this tool based on the <purpose />, the chat history, and the output of previous steps.";

// Type for steps without the update function (used for JSON serialization)
type SerializableStep = Omit<Step, "update">;

// Needs to stay aligned with server/internal/templates/impl.go:CustomToolJSONV1
type CustomTool = {
  toolName: string;
  purpose: string;
  inputs: Input[];
  steps: SerializableStep[];
};

export function ToolBuilderNew() {
  const ctx = useToolifyContext();

  const newTemplate: ToolBuilderState = {
    name: "new_composite_tool",
    description:
      "Do a series of steps using the tools in a toolset to accomplish a task",
    purpose:
      "Do a series of steps using the tools in a toolset to accomplish a task",
    inputs: [],
    steps: [],
  };

  // If we came from the toolify dialog, pull in the suggestion
  if (ctx.toolset) {
    newTemplate.toolset = ctx.toolset;
    newTemplate.name = slugify(ctx.suggestion.name);
    newTemplate.description = ctx.suggestion.description;
    newTemplate.purpose = ctx.purpose;
    newTemplate.inputs = ctx.suggestion.inputs;
    newTemplate.steps = ctx.suggestion.steps.map((step) => ({
      ...step,
      id: uuidv7(),
      canonicalTool: step.tool,
      update: () => {}, // Set later
    }));
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
      </Page.Header>
      <Page.Body fullWidth fullHeight>
        <ToolBuilder initial={newTemplate} />
      </Page.Body>
    </Page>
  );
}

export function ToolBuilderPage() {
  const { toolName } = useParams();

  const { data: toolsets } = useListToolsetsSuspense();

  // TODO: This is a little janky
  const toolset =
    toolsets?.toolsets.find((t) =>
      t.tools.some((tool) => tool.name === toolName),
    ) ?? undefined;

  const { data: template } = useTemplateSuspense({
    name: toolName,
  });

  const parsed = parsePrompt(template.template.prompt);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          fullWidth
          substitutions={{ [toolName ?? ""]: template.template.name }}
        />
      </Page.Header>
      <Page.Body fullWidth fullHeight>
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

type ToolBuilderState = {
  id?: string;
  historyId?: string;
  name: string;
  description: string;
  purpose: string;
  inputs: Input[];
  steps: Step[];
  toolset?: ToolsetEntry;
};

function ToolBuilder({ initial }: { initial: ToolBuilderState }) {
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const routes = useRoutes();
  const telemetry = useTelemetry();

  const [name, setName] = useState(initial.name);
  const [description, setDescription] = useState(initial.description);
  const [purpose, setPurpose] = useState(initial.purpose);
  const [inputs, setInputs] = useState<Input[]>(initial.inputs);
  const [toolsetFilter, setToolset] = useState<ToolsetEntry | undefined>(
    initial.toolset,
  );

  const { data: toolsetData } = useToolset(toolsetFilter?.slug);

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

  let tools = toolsetData?.tools ?? [];
  tools = tools.filter((t) => t.id !== initial.id); // Make sure you can't create recursive tools

  // Ensures that the canonical tool, tool URN, and update function is set for the step
  const makeStep = (step: Step) => {
    if (!step.tool || !tools.length) {
      return {
        ...step,
        update: (s: Step) => setStep(s),
      };
    }

    const tool = tools.find((t) => t.name === step.tool);
    const canonicalToolName = step.canonicalTool ?? tool?.canonicalName;
    const toolUrn = step.toolUrn ?? tool?.toolUrn;

    if (!canonicalToolName) console.error(`Tool ${step.tool} not found`);
    if (!toolUrn) console.error(`Tool URN for ${step.tool} not found`);

    return {
      ...step,
      update: (s: Step) => setStep(s),
      canonicalTool: canonicalToolName,
      toolUrn,
    };
  };

  const [steps, setSteps] = useState<Step[]>(initial.steps.map(makeStep));

  useEffect(() => {
    setName(initial.name);
    setDescription(initial.description);
    setPurpose(initial.purpose);
    setInputs(initial.inputs);
    setSteps(initial.steps.map(makeStep));
  }, [initial]);

  const insertTool = (
    tool: { name: string; canonicalName: string; toolUrn: string } | "none",
  ) => {
    if (steps.length >= 10) {
      return;
    }
    const newSteps = [...steps];

    const step: Step = {
      id: uuidv7(),
      instructions: instructionsPlaceholder,
      update: (step) => setStep(step),
    };

    if (tool !== "none") {
      step.tool = tool.name;
      step.canonicalTool = tool.canonicalName;
      step.toolUrn = tool.toolUrn;
    } else {
      step.instructions = "Fill in what this step should do...";
    }

    newSteps.push(step);
    setSteps(newSteps);

    telemetry.capture("tool_builder_event", {
      event: "add_step",
      tool: tool !== "none" ? tool.name : "none",
    });
  };

  // When purpose or steps change, recompute inputs
  useEffect(() => {
    const allInputs = steps.flatMap((step) => parseInputs(step.instructions));
    allInputs.push(...parseInputs(purpose));
    const currentInputs = inputs.map((input) => input.name);

    const curFiltered = inputs.filter((input) =>
      allInputs.includes(input.name),
    );
    const toInsert = allInputs.filter(
      (input) => !currentInputs.includes(input),
    );
    setInputs([...curFiltered, ...toInsert.map((input) => ({ name: input }))]);
  }, [steps, purpose]);

  const validateName = (v: string) => {
    if (v.length < 4) {
      return "Tool name must be at least 4 characters long";
    }
    if (v.length >= 40) {
      return "Tool name must be less than or equal to 40 characters long";
    }
    if (tools.some((t) => t.name.toLowerCase() === v.toLowerCase())) {
      return "Tool name must be unique";
    }
    if (!v.match(TOOL_NAME_REGEX)) {
      return "Tool name must contain only lowercase letters, numbers, underscores, and hyphens";
    }
    return true;
  };

  const AddStepButton = () => {
    const [open, setOpen] = useState(false);

    return (
      <ToolSelectPopover
        open={open}
        setOpen={setOpen}
        tools={tools}
        onSelect={(tool) => {
          insertTool(tool);
          setOpen(false);
        }}
      >
        <Button
          variant="secondary"
          size="sm"
          className={
            "bg-card dark:bg-background border-stone-300 dark:border-stone-700 border-1"
          }
          disabled={steps.length >= 10}
        >
          <Button.LeftIcon>
            <Icon name="plus" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Add Step</Button.Text>
        </Button>
      </ToolSelectPopover>
    );
  };

  const anyChanges =
    name !== initial.name ||
    description !== initial.description ||
    purpose !== initial.purpose ||
    inputs.length !== initial.inputs.length ||
    steps.length !== initial.steps.length ||
    inputs.some(
      (input) =>
        input.description !==
        initial.inputs.find((i) => i.name === input.name)?.description,
    ) ||
    steps.some(
      (step) =>
        step.instructions !==
        initial.steps.find((s) => s.id === step.id)?.instructions,
    );

  const revertButton = anyChanges && (
    <Button
      variant="tertiary"
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
      invalidateAllTemplate(queryClient);
      invalidateAllTemplates(queryClient);
    },
    onError: (error) => {
      handleAPIError(error, "Failed to update tool");
    },
  });

  const saveButton = (
    <Button
      disabled={!!initial.id && !anyChanges}
      onClick={async () => {
        try {
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
              ]),
            ),
            required: inputs.map((input) => input.name),
          };

          const higherOrderTool: CustomTool = {
            toolName: name,
            purpose,
            inputs,
            steps,
          };

          if (initial.id) {
            updatePrompt({
              request: {
                updatePromptTemplateForm: {
                  id: initial.id,
                  name,
                  description,
                  prompt: higherOrderToolToJSON(higherOrderTool),
                  arguments: JSON.stringify(argsJsonSchema),
                  toolsHint: steps.flatMap((step) => step.canonicalTool ?? []),
                  toolUrnsHint: steps.flatMap((step) => step.toolUrn ?? []),
                },
              },
            });

            telemetry.capture("tool_builder_event", {
              event: "update_tool",
            });
          } else {
            const template = await client.templates.create({
              createPromptTemplateForm: {
                name,
                description,
                kind: PromptTemplateKind.HigherOrderTool,
                prompt: higherOrderToolToJSON(higherOrderTool),
                arguments: JSON.stringify(argsJsonSchema),
                toolsHint: steps.flatMap((step) => step.canonicalTool ?? []),
                engine: "mustache",
              },
            });

            telemetry.capture("tool_builder_event", {
              event: "update_tool",
            });

            // Automatically add to the toolset
            await client.toolsets.updateBySlug({
              slug: toolsetFilter?.slug ?? "",
              updateToolsetRequestBody: {
                toolUrns: [
                  ...(toolsetData?.toolUrns ?? []),
                  template.template.toolUrn,
                ],
              },
            });

            invalidateAllListToolsets(queryClient);
            routes.customTools.toolBuilder.goTo(name);
          }

          toast.success("Tool saved successfully", { persist: true });
        } catch (error) {
          handleAPIError(error, "Failed to save tool");
        }
      }}
    >
      <Button.LeftIcon>
        <Icon name="save" className="h-4 w-4" />
      </Button.LeftIcon>
      <Button.Text>Save</Button.Text>
    </Button>
  );

  const toolName = (
    <EditableText
      label="Tool Name"
      description="Give your tool a name. This influences tool selection."
      value={name}
      onSubmit={setName}
      validate={validateName}
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
        <Stack gap={1} className="h-full overflow-y-scroll">
          <Stack direction="horizontal" align="center" className="w-full">
            <Block label="Tool name" className="w-2/3">
              <BlockInner>{toolName}</BlockInner>
            </Block>
            <Block label="Toolset" className="w-1/3">
              <BlockInner>
                <ToolsetDropdown
                  selectedToolset={toolsetFilter}
                  setSelectedToolset={(toolset) => setToolset(toolset)}
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
                <MustacheHighlight>{purpose}</MustacheHighlight>
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
                          i.name === input.name ? { ...i, description } : i,
                        ),
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
                    tools={tools}
                    remove={() =>
                      setSteps(steps.filter((s) => s.id !== step.id))
                    }
                    moveUp={
                      index > 0
                        ? () => {
                            const newSteps = [...steps];
                            const temp = newSteps[index]!;
                            newSteps[index] = newSteps[index - 1]!;
                            newSteps[index - 1] = temp;
                            setSteps(newSteps);
                          }
                        : undefined
                    }
                    moveDown={
                      index < steps.length - 1
                        ? () => {
                            const newSteps = [...steps];
                            const temp = newSteps[index]!;
                            newSteps[index] = newSteps[index + 1]!;
                            newSteps[index + 1] = temp;
                            setSteps(newSteps);
                          }
                        : undefined
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
            className="mt-4 mb-8"
          >
            {revertButton}
            {saveButton}
          </Stack>
        </Stack>
      </ResizablePanel.Pane>
      <ResizablePanel.Pane minSize={35}>
        <ChatProvider>
          <ChatPanel
            toolsetSlug={toolsetFilter?.slug}
            defaultEnvironmentSlug={toolsetData?.defaultEnvironmentSlug}
            inputs={inputs}
            steps={steps}
            name={name}
            purpose={purpose}
          />
        </ChatProvider>
      </ResizablePanel.Pane>
    </ResizablePanel>
  );
}

const inputStyles =
  "bg-blue-100 dark:bg-blue-900 text-blue-900 dark:text-blue-100 px-1 rounded";
const blockBackground = "bg-stone-100 dark:bg-stone-900";

export const MustacheHighlight = ({
  children,
}: {
  children: React.ReactNode;
}) => {
  if (typeof children !== "string") return <>{children}</>;

  let start = 0;
  const chunks: React.ReactNode[] = [];
  for (const part of children.matchAll(MUSTACHE_VAR_REGEX)) {
    const text = children.slice(start, part.index);
    if (text) {
      chunks.push(<span key={`text-${start}`}>{text}</span>);
    }

    chunks.push(
      <span key={`var-${start}`} className={inputStyles}>
        {part[0].slice(2, -2)}
      </span>,
    );

    start = part.index + part[0].length;
  }

  chunks.push(<span key={`text-${start}`}>{children.slice(start)}</span>);

  return chunks;
};

const StepCard = ({
  step,
  tools,
  remove,
  moveUp,
  moveDown,
}: {
  step: Step;
  tools: Tool[];
  remove: () => void;
  moveUp?: () => void;
  moveDown?: () => void;
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
        if (tool === "none") {
          return;
        }

        setOpen(false);
        step.update?.({
          ...step,
          tool: tool.name,
          canonicalTool: tool.canonicalName,
        });
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

  let noToolText = "Then...";
  if (moveUp && !moveDown) {
    noToolText = "Finally...";
  } else if (!moveUp && moveDown) {
    noToolText = "First...";
  }

  return (
    <BlockInner className="p-0 rounded-md overflow-clip">
      <Stack>
        <Stack
          direction="horizontal"
          align="center"
          justify="space-between"
          className="px-4 py-3 border-b border-stone-300 dark:border-stone-700 group/heading"
        >
          {step.canonicalTool ? (
            <Type variant="subheading">Use the {toolBadge} tool to...</Type>
          ) : (
            <Type variant="subheading">{noToolText}</Type>
          )}
          <Stack
            direction="horizontal"
            className="mr-[-8px] mt-[-8px] group-hover/heading:opacity-100 opacity-0 trans"
          >
            {moveUp && (
              <Button
                variant="tertiary"
                size="xs"
                onClick={moveUp}
                className="mr-[-4px]"
                aria-label="Move up"
              >
                <Button.Icon>
                  <Icon name="arrow-up" className="h-3 w-3" />
                </Button.Icon>
              </Button>
            )}
            {moveDown && (
              <Button
                variant="tertiary"
                size="xs"
                onClick={moveDown}
                className="mr-[-4px]"
                aria-label="Move down"
              >
                <Button.Icon>
                  <Icon name="arrow-down" className="h-3 w-3" />
                </Button.Icon>
              </Button>
            )}
            <DeleteButton size="sm" tooltip="Delete step" onClick={remove} />
          </Stack>
        </Stack>
        <div className={cn("px-4 py-3", blockBackground)}>
          <EditableText
            label="Instructions"
            description="Describe what this step should do. Use {{curly braces}} to declare inputs"
            value={step.instructions}
            lines={3}
            onSubmit={(instructions) => {
              step.update?.({ ...step, instructions });
            }}
          >
            <Type
              small
              className={cn(
                step.instructions === instructionsPlaceholder &&
                  "italic text-muted-foreground!",
              )}
            >
              <MustacheHighlight>{step.instructions}</MustacheHighlight>
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
  onSelect: (tool: Tool | "none") => void;
  tools: Tool[];
  children: React.ReactNode;
}) => {
  const groupedTools = useGroupedTools(tools);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent className="w-[200px] min-w-fit p-0">
        <Command>
          <CommandInput placeholder="Search..." className="h-9" />
          <CommandList>
            <CommandEmpty>
              {groupedTools.length === 0
                ? "Toolset is empty."
                : "No items found."}
            </CommandEmpty>
            <CommandGroup>
              <SimpleTooltip tooltip="Create a step that doesn't use any tools">
                <CommandItem
                  value={"none"}
                  className="cursor-pointer min-w-fit text-sm"
                  onSelect={() => onSelect("none")}
                >
                  <Icon name="file-text" size="small" />
                  Instruction
                </CommandItem>
              </SimpleTooltip>
            </CommandGroup>
            {groupedTools.map((group) => (
              <CommandGroup
                key={group.key}
                heading={capitalize(group.key)}
                className="overflow-x-scroll"
              >
                {group.tools.map((tool) => (
                  <CommandItem
                    key={tool.name}
                    value={tool.name}
                    className="cursor-pointer min-w-fit"
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

const parsePrompt = (
  prompt: string,
): { purpose: string; inputs: Input[]; steps: Step[] } => {
  const customTool = JSON.parse(prompt) as CustomTool;

  const steps: Step[] = customTool.steps.map((step) => ({
    ...step,
    id: step.id || uuidv7(), // Ensure steps have IDs
    update: () => {
      console.error("update not implemented");
    }, // This will be replaced by the component when used
  }));

  return {
    purpose: customTool.purpose,
    inputs: customTool.inputs,
    steps,
  };
};

const customToolSystemPrompt = [
  {
    id: "1",
    role: "system" as const,
    parts: [
      {
        type: "text" as const,
        text: "Use this chat to test out the custom tool the user has built. You should faithfuly execute the plan it sets out to achieve the specified purpose.",
      },
    ],
  },
];

function ChatPanel(props: {
  toolsetSlug?: string;
  defaultEnvironmentSlug?: string;
  inputs: Input[];
  steps: Step[];
  name: string;
  purpose: string;
}) {
  const { toolsetSlug, defaultEnvironmentSlug, inputs, steps, name, purpose } =
    props;
  const client = useSdkClient();
  const chat = useChatContext();
  const telemetry = useTelemetry();
  const [selectedEnvironment, setSelectedEnvironment] = useState(
    defaultEnvironmentSlug ?? null,
  );

  const chatConfigRef: ChatConfig = useRef({
    toolsetSlug: toolsetSlug ?? null,
    environmentSlug: selectedEnvironment ?? null,
    isOnboarding: false,
  });

  useEffect(() => {
    if (toolsetSlug) {
      chatConfigRef.current.toolsetSlug = toolsetSlug;
    }
  }, [toolsetSlug]);

  useEffect(() => {
    if (!chatConfigRef.current.environmentSlug && defaultEnvironmentSlug) {
      chatConfigRef.current.environmentSlug = defaultEnvironmentSlug;
      setSelectedEnvironment(defaultEnvironmentSlug);
    }
  }, [defaultEnvironmentSlug]);

  const environmentSwitcher = (
    <EnvironmentDropdown
      selectedEnvironment={selectedEnvironment}
      setSelectedEnvironment={(slug) => {
        setSelectedEnvironment(slug);
        chatConfigRef.current.environmentSlug = slug;
      }}
      className="h-7"
      visibilityThreshold={2}
    />
  );

  const tryNowButton = (
    <Button
      size="sm"
      className="h-7"
      onClick={async () => {
        telemetry.capture("tool_builder_event", {
          event: "try_now",
        });

        const inputArgs = Object.fromEntries(
          inputs.map((input) => [input.name, `{{${input.name}}}`]),
        );

        const higherOrderTool: CustomTool = {
          toolName: name,
          purpose,
          inputs,
          steps,
        };

        const renderResult = await client.templates.render({
          renderTemplateRequestBody: {
            prompt: higherOrderToolToJSON(higherOrderTool),
            arguments: inputArgs,
            engine: "mustache",
            kind: PromptTemplateKind.HigherOrderTool,
          },
        });

        const renderedPrompt = renderResult.prompt || "";

        chat.appendMessage({
          content: `\`\`\`xml\n${renderedPrompt}\n\`\`\``,
        });
      }}
    >
      <Button.LeftIcon>
        <Icon name="play" className="h-4 w-4" />
      </Button.LeftIcon>
      <Button.Text>Try Now</Button.Text>
    </Button>
  );

  const additionalActions = (
    <>
      {tryNowButton}
      {environmentSwitcher}
    </>
  );

  return (
    <ChatWindow
      configRef={chatConfigRef}
      additionalActions={additionalActions}
      initialMessages={customToolSystemPrompt}
      hideTemperatureSlider
    />
  );
}
