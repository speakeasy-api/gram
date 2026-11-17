import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useAssistantsUpdateMutation } from "@gram/client/react-query/assistantsUpdate.js";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Section } from "./PanelSection";
import { PersonalitySelect } from "./PersonalitySelect";
import { ASSISTANT_INSTRUCTIONS_SECTION_ID } from "./sectionIds";
import {
  joinInstructions,
  splitInstructions,
  type InstructionParts,
} from "./sections";

/**
 * What the assistant does and how it sounds, edited as the sections its
 * prompt is built from. Behavior is written from its tools, so it shows
 * read-only. Everything waits for Save.
 */
export function InstructionsSection({
  assistant,
  onUpdated,
}: {
  assistant: Assistant;
  onUpdated: () => void;
}): JSX.Element {
  const project = useProject();
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("assistant:write", assistant.id, project.id);

  const saved = editableParts(assistant.instructions);
  const [draft, setDraft] = useState<InstructionParts>(saved);
  const [preset, setPreset] = useState<string>();
  // A save from elsewhere (another tab, the Slack setup) resets the form.
  const [savedText, setSavedText] = useState(assistant.instructions);
  if (savedText !== assistant.instructions) {
    setSavedText(assistant.instructions);
    setDraft(saved);
    setPreset(undefined);
  }

  const next = joinInstructions(draft);
  const dirty = next !== joinInstructions(saved);

  const update = useAssistantsUpdateMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
      onUpdated();
      toast.success("Instructions saved");
    },
    onError: () => toast.error("Couldn't save the instructions. Try again."),
  });

  const set = (key: keyof InstructionParts) => (value: string) =>
    setDraft((prev) => ({ ...prev, [key]: value }));

  const disabled = !canWrite || update.isPending;

  return (
    <Section
      id={ASSISTANT_INSTRUCTIONS_SECTION_ID}
      title="Instructions"

      footer={
        canWrite ? (
          <div className="ml-auto flex items-center gap-2">
            {dirty ? (
              <Button
                variant="tertiary"
                onClick={() => {
                  setDraft(saved);
                  setPreset(undefined);
                }}
                disabled={update.isPending}
              >
                Discard
              </Button>
            ) : null}
            <Button
              onClick={() =>
                update.mutate({
                  request: {
                    updateAssistantForm: {
                      id: assistant.id,
                      instructions: next,
                    },
                  },
                })
              }
              disabled={!dirty || update.isPending || next.trim() === ""}
            >
              {update.isPending ? "Saving…" : "Save"}
            </Button>
          </div>
        ) : (
          <Text small muted>
            Read-only.
          </Text>
        )
      }
    >
      <div className="flex max-w-3xl flex-col gap-6">
        <Field id="assistant-tasks" label="What it does">
          <TextArea
            id="assistant-tasks"
            value={draft.Tasks}
            onChange={set("Tasks")}
            rows={8}
            disabled={disabled}
            placeholder="e.g. Triage new bug reports in #support."
          />
        </Field>

        <Field
          id="assistant-personality"
          label="Personality"

          action={
            <PersonalitySelect
              value={preset}
              disabled={disabled}
              className="w-56"
              onPick={(p) => {
                setPreset(p.slug);
                set("Personality")(p.instructions);
              }}
            />
          }
        >
          <TextArea
            id="assistant-personality"
            value={draft.Personality}
            onChange={(value) => {
              setPreset(undefined);
              set("Personality")(value);
            }}
            rows={8}
            disabled={disabled}
          />
        </Field>

        {/* Older assistants keep their whole prompt outside the sections. */}
        {saved.preamble ? (
          <Field
            id="assistant-other-instructions"
            label="Other instructions"
            hint="Sent before the sections above."
          >
            <TextArea
              id="assistant-other-instructions"
              value={draft.preamble}
              onChange={set("preamble")}
              rows={8}
              disabled={disabled}
            />
          </Field>
        ) : null}

        {draft.Behavior ? (
          <Collapsible>
            <CollapsibleTrigger className="text-muted-foreground hover:text-foreground group flex items-center gap-1 text-sm">
              <ChevronRight className="size-4 transition-transform group-data-[state=open]:rotate-90" />
              Slack rules (automatic)
            </CollapsibleTrigger>
            <CollapsibleContent>
              <pre className="bg-muted/30 border-border mt-2 border p-3 font-mono text-xs whitespace-pre-wrap">
                {draft.Behavior}
              </pre>
            </CollapsibleContent>
          </Collapsible>
        ) : null}
      </div>
    </Section>
  );
}

/**
 * A prompt written before sections existed is all one block; it reads as
 * what the assistant does, and saving files it under that section.
 */
function editableParts(text: string): InstructionParts {
  const parts = splitInstructions(text);
  if (parts.preamble && !parts.Personality && !parts.Tasks) {
    return { ...parts, preamble: "", Tasks: parts.preamble };
  }
  return parts;
}

function Field({
  id,
  label,
  hint,
  action,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-4">
        <div className="flex flex-col gap-0.5">
          <Label htmlFor={id}>{label}</Label>
          {hint ? (
            <Text small muted>
              {hint}
            </Text>
          ) : null}
        </div>
        {action}
      </div>
      {children}
    </div>
  );
}
