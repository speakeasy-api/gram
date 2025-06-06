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
import { useGroupedTools } from "@/lib/toolNames";
import { capitalize, cn } from "@/lib/utils";
import { HTTPToolDefinition, Toolset } from "@gram/client/models/components";
import { useToolset } from "@gram/client/react-query";
import { useListTools } from "@gram/client/react-query/listTools.js";
import { ResizablePanel, Stack } from "@speakeasy-api/moonshine";
import { Message } from "ai";
import { useEffect, useRef, useState } from "react";
import { v4 as uuidv4 } from "uuid";
import { ChatProvider, useChatContext } from "../playground/ChatContext";
import { ChatConfig, ChatWindow } from "../playground/ChatWindow";
import { ToolsetDropdown } from "../toolsets/ToolsetDropown";

type Input = {
  name: string;
  description?: string;
};

type Step = {
  id: string;
  tool: string;
  instructions: string;
  inputs?: string[];
  update: (step: Step) => void;
};

const instructionsPlaceholder =
  "Interpret what to do with this tool based on the <purpose />, the chat history, and the output of previous steps.";

export default function ToolBuilderPage() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <ChatProvider>
          <ToolBuilder />
        </ChatProvider>
      </Page.Body>
    </Page>
  );
}

function ToolBuilder() {
  const [name, setName] = useState("New Composite Tool");
  const [description, setDescription] = useState("A new composite tool");
  const [purpose, setPurpose] = useState(
    "Do a series of steps using the tools in a toolset to accomplish a task"
  );
  const [steps, setSteps] = useState<Step[]>([]);
  const [inputs, setInputs] = useState<Input[]>([]);
  const chat = useChatContext();

  const setStep = (step: Step) => {
    const newInputs = step.instructions.match(/(\{\{[^}]+\}\})/g);
    step.inputs = newInputs?.map((input) => input.slice(2, -2));

    setSteps((prev) => {
      const newSteps = [...prev];
      const index = newSteps.findIndex((s) => s.id === step.id);
      newSteps[index] = step;
      return newSteps;
    });
  };

  const [toolsetFilter, setToolset] = useState<Toolset>();

  const insertTool = (tool: { name: string }) => {
    if (steps.length >= 10) {
      return;
    }
    const newSteps = [...steps];
    newSteps.push({
      id: uuidv4(),
      tool: tool.name,
      instructions: instructionsPlaceholder,
      update: (step) => setStep(step),
    });
    setSteps(newSteps);
  };

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

  useEffect(() => {
    const inputs = steps.flatMap((step) => {
      const inputs = step.instructions.match(/(\{\{[^}]+\}\})/g);
      if (inputs) {
        return inputs.map((input) => ({ name: input.slice(2, -2) }));
      }
      return [];
    });
    setInputs(inputs);
  }, [steps]);

  const validateName = (v: string) => {
    if (v.length < 3) {
      return "Tool name must be at least 3 characters long";
    }
    if (v.length > 100) {
      return "Tool name must be less than 100 characters long";
    }
    if (tools?.tools.some((t) => t.name.toLowerCase() === v.toLowerCase())) {
      return "Tool name must be unique";
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
    if (!maybeFilteredTools) {
      return null;
    }

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
          content: buildPrompt(purpose, inputs, steps),
        })
      }
    >
      Try Now
    </Button>
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
              <BlockInner>
                <EditableText
                  label="Tool Name"
                  description="Give your tool a name. This influences tool selection."
                  value={name}
                  onSubmit={setName}
                  validate={validateName}
                >
                  <Heading variant="h3">{name}</Heading>
                </EditableText>
              </BlockInner>
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
                <>
                  <StepCard
                    key={index}
                    step={step}
                    tools={maybeFilteredTools}
                    removeStep={() =>
                      setSteps(steps.filter((s) => s.id !== step.id))
                    }
                  />
                  <StepSeparator />
                </>
              ))}
              <AddStepButton />
            </Stack>
          </Block>
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
const Block = ({
  label,
  labelRHS,
  className,
  children,
}: {
  label: string;
  labelRHS?: string;
  className?: string;
  children: React.ReactNode;
}) => {
  return (
    <Stack
      className={cn("p-1 rounded-md w-full", className)}
      align={labelRHS ? "stretch" : "start"}
    >
      <Stack
        direction="horizontal"
        align="center"
        justify="space-between"
        className={cn(
          "px-2 pt-1 rounded-sm rounded-b-none",
          blockBackground,
          !labelRHS && "mb-[-3px]"
        )}
      >
        <Type variant="small">{label}</Type>
        {labelRHS && (
          <Type muted variant="small">
            {labelRHS}
          </Type>
        )}
      </Stack>
      <div
        className={cn(
          "h-full w-full p-1 rounded-md rounded-tl-none",
          blockBackground,
          labelRHS && "rounded-tr-none"
        )}
      >
        {children}
      </div>
    </Stack>
  );
};

const BlockInner = ({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) => {
  return (
    <div
      className={cn(
        "bg-card dark:bg-background rounded-sm p-2 border-1 border-stone-300 dark:border-stone-700",
        className
      )}
    >
      {children}
    </div>
  );
};

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
    .map(
      (step) => `  <CallTool tool_name="${step.tool}">
    <Instruction>${step.instructions.trim()}</Instruction>
    ${step.inputs?.map((input) => `<Input name="${input}" />`).join("\n")}
  </CallTool>`
    )
    .join("\n");

  return `
\`\`\`xml
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
${inputsPortion || "No inputs needed"}
</Inputs>
<Plan>
${stepsPortion}
</Plan>
\`\`\``;
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
