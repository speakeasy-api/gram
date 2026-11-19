import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Icon } from "@/components/ui/Icon";
import { Input } from "@/components/ui/Input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Spinner } from "@/components/ui/Spinner";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import type { McpFeedbackSettings } from "@gram/client/models/components/mcpfeedbacksettings.js";
import { useState } from "react";

// The limits the API enforces, so a value it would refuse never leaves the form.
const MAX_TOOL_NAME_LENGTH = 64;
const MAX_INSTRUCTIONS_LENGTH = 2000;
const TOOL_NAME_PATTERN = /^[A-Za-z0-9_-]+$/;

/** What the sheet saves: a value left at its default is sent as unset. */
export type FeedbackToolCustomization = {
  toolName: string | undefined;
  toolDescription: string | undefined;
};

/**
 * Changes what agents see of the feedback tool: its name and the instructions
 * they read to decide when to call it. Both start from the defaults, so a
 * server that needs nothing special never opens this.
 */
export function CustomizeFeedbackToolSheet({
  settings,
  open,
  saving,
  onOpenChange,
  onSave,
}: {
  settings: McpFeedbackSettings;
  open: boolean;
  saving: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (customization: FeedbackToolCustomization) => void;
}): JSX.Element {
  const savedName = settings.toolName ?? settings.defaultToolName;
  const savedInstructions =
    settings.toolDescription ?? settings.defaultToolDescription;
  const [name, setName] = useState(savedName);
  const [instructions, setInstructions] = useState(savedInstructions);
  const [confirmingDiscard, setConfirmingDiscard] = useState(false);

  const trimmedName = name.trim();
  const trimmedInstructions = instructions.trim();
  const nameError = toolNameError(trimmedName);
  const instructionsError = instructionsProblem(trimmedInstructions);
  const dirty =
    trimmedName !== savedName || trimmedInstructions !== savedInstructions;

  const requestClose = () => {
    if (saving) return;
    if (dirty) {
      setConfirmingDiscard(true);
      return;
    }
    onOpenChange(false);
  };

  return (
    <>
      <Sheet
        open={open}
        onOpenChange={(next) => {
          if (!next) requestClose();
        }}
      >
        <SheetContent
          side="right"
          className="flex w-full flex-col gap-0 sm:max-w-xl"
        >
          <SheetHeader className="px-6 pt-6 pb-0">
            <SheetTitle className="text-lg">
              Customize the feedback tool
            </SheetTitle>
            <SheetDescription>
              Change what agents see on this server. Reports are collected the
              same way whatever you call the tool.
            </SheetDescription>
          </SheetHeader>

          <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
            <Field>
              <div className="flex min-h-7 items-center justify-between gap-3">
                <FieldLabel htmlFor="feedback-tool-name">Tool name</FieldLabel>
                {trimmedName !== settings.defaultToolName && (
                  <UseDefaultButton
                    disabled={saving}
                    onClick={() => setName(settings.defaultToolName)}
                  />
                )}
              </div>
              <Input
                id="feedback-tool-name"
                value={name}
                onChange={setName}
                disabled={saving}
                className="font-mono"
                aria-invalid={nameError !== undefined}
              />
              {nameError ? (
                <FieldError>{nameError}</FieldError>
              ) : (
                <FieldDescription>
                  Letters, numbers, underscores, and hyphens. If the server
                  already has a tool with this name, agents get this one
                  instead.
                </FieldDescription>
              )}
            </Field>

            <Field>
              <div className="flex min-h-7 items-center justify-between gap-3">
                <FieldLabel htmlFor="feedback-tool-instructions">
                  Instructions for agents
                </FieldLabel>
                {trimmedInstructions !== settings.defaultToolDescription && (
                  <UseDefaultButton
                    disabled={saving}
                    onClick={() =>
                      setInstructions(settings.defaultToolDescription)
                    }
                  />
                )}
              </div>
              <TextArea
                id="feedback-tool-instructions"
                value={instructions}
                onChange={setInstructions}
                disabled={saving}
                rows={14}
                className="text-sm"
                aria-invalid={instructionsError !== undefined}
              />
              {instructionsError ? (
                <FieldError>{instructionsError}</FieldError>
              ) : (
                <FieldDescription>
                  Agents read this to decide when to send feedback and what to
                  put in it. {trimmedInstructions.length.toLocaleString()} of{" "}
                  {MAX_INSTRUCTIONS_LENGTH.toLocaleString()} characters.
                </FieldDescription>
              )}
            </Field>

            <ReportFields inputSchema={settings.tool.inputSchema} />
          </div>

          <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
            <Button
              variant="secondary"
              disabled={saving}
              onClick={requestClose}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="primary"
              disabled={
                saving ||
                !dirty ||
                nameError !== undefined ||
                instructionsError !== undefined
              }
              onClick={() =>
                onSave({
                  toolName:
                    trimmedName === settings.defaultToolName
                      ? undefined
                      : trimmedName,
                  toolDescription:
                    trimmedInstructions === settings.defaultToolDescription
                      ? undefined
                      : trimmedInstructions,
                })
              }
            >
              {saving && (
                <Button.LeftIcon>
                  <Spinner className="mr-0 size-4" />
                </Button.LeftIcon>
              )}
              <Button.Text>{saving ? "Saving" : "Save"}</Button.Text>
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <ConfirmDialog
        open={confirmingDiscard}
        onOpenChange={setConfirmingDiscard}
        title="Discard your changes?"
        description="The feedback tool keeps the name and instructions it has now."
        confirmLabel="Discard"
        cancelLabel="Keep editing"
        onConfirm={() => {
          setConfirmingDiscard(false);
          onOpenChange(false);
        }}
      />
    </>
  );
}

function UseDefaultButton({
  disabled,
  onClick,
}: {
  disabled: boolean;
  onClick: () => void;
}): JSX.Element {
  return (
    <Button variant="tertiary" size="xs" disabled={disabled} onClick={onClick}>
      <Button.Text>Use the default</Button.Text>
    </Button>
  );
}

function toolNameError(name: string): string | undefined {
  if (name === "") return "Give the tool a name.";
  if (name.length > MAX_TOOL_NAME_LENGTH) {
    return `Use ${MAX_TOOL_NAME_LENGTH} characters or fewer.`;
  }
  if (!TOOL_NAME_PATTERN.test(name)) {
    return "Use only letters, numbers, underscores, and hyphens.";
  }
  return undefined;
}

function instructionsProblem(instructions: string): string | undefined {
  if (instructions === "") return "Tell agents when to send feedback.";
  if (instructions.length > MAX_INSTRUCTIONS_LENGTH) {
    return `Use ${MAX_INSTRUCTIONS_LENGTH.toLocaleString()} characters or fewer. This is ${instructions.length.toLocaleString()}.`;
  }
  return undefined;
}

type ReportField = { name: string; description: string; required: boolean };

/** The fields of the tool's input, from the schema agents are given. */
function reportFields(inputSchema: string): ReportField[] {
  let schema: unknown;
  try {
    schema = JSON.parse(inputSchema);
  } catch {
    return [];
  }
  if (typeof schema !== "object" || schema === null) return [];
  const { properties, required } = schema as {
    properties?: unknown;
    required?: unknown;
  };
  if (typeof properties !== "object" || properties === null) return [];
  const requiredNames = Array.isArray(required) ? required : [];
  return Object.entries(properties).map(([name, property]) => ({
    name,
    description:
      typeof property === "object" &&
      property !== null &&
      "description" in property &&
      typeof property.description === "string"
        ? property.description
        : "",
    required: requiredNames.includes(name),
  }));
}

/**
 * What a report can hold. Closed by default: it is the tool's schema, which
 * matters to someone checking what leaves an agent, not to someone renaming it.
 */
function ReportFields({
  inputSchema,
}: {
  inputSchema: string;
}): JSX.Element | null {
  const fields = reportFields(inputSchema);
  if (fields.length === 0) return null;
  return (
    <Collapsible>
      <CollapsibleTrigger className="group text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-sm">
        <Icon
          name="chevron-right"
          className="size-4 transition-transform group-data-[state=open]:rotate-90"
        />
        Technical details
      </CollapsibleTrigger>
      <CollapsibleContent>
        <Text small muted className="mt-3 block">
          What an agent can put in a report. These fields are the same on every
          server.
        </Text>
        <dl className="mt-3 space-y-3">
          {fields.map((field) => (
            <div key={field.name}>
              <dt className="font-mono text-sm">
                {field.name}
                {field.required && (
                  <span className="text-muted-foreground font-sans">
                    {" "}
                    · required
                  </span>
                )}
              </dt>
              <dd className="text-muted-foreground text-sm">
                {field.description}
              </dd>
            </div>
          ))}
        </dl>
      </CollapsibleContent>
    </Collapsible>
  );
}
