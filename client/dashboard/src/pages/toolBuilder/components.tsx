import { DeleteButton } from "@/components/delete-button";
import { EditableText } from "@/components/editable-text";
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
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Type } from "@/components/ui/type";
import { useGroupedTools } from "@/lib/toolNames";
import { capitalize, cn } from "@/lib/utils";
import { HTTPToolDefinition } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";

export type Input = {
  name: string;
  description?: string;
};

export type Step = {
  id: string;
  tool: string;
  instructions: string;
  inputs?: string[];
  update: (step: Step) => void;
};

export const blockBackground = "bg-stone-100 dark:bg-stone-900";
export const Block = ({
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

export const BlockInner = ({
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

export const instructionsPlaceholder =
  "Interpret what to do with this tool based on the <purpose />, the chat history, and the output of previous steps.";

export const StepCard = ({
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

export const StepSeparator = () => {
  return (
    <div className="h-4 w-1/2 border-r-2 border-dashed border-stone-400 dark:border-stone-600" />
  );
};
