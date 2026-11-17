import { ModelSelect } from "@/components/model-select";
import { FormPage } from "@/components/page-templates";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import { useProject } from "@/contexts/Auth";
import { DEFAULT_ASSISTANT_MODEL } from "@/lib/models";
import { useRoutes } from "@/routes";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useAssistantsCreateMutation } from "@gram/client/react-query/assistantsCreate.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { computeBehaviorSection } from "./detail/behaviors";
import { PersonalitySelect } from "./detail/PersonalitySelect";
import { ReasoningEffortSelect } from "./detail/ReasoningEffortSelect";
import {
  REASONING_EFFORT_HELP,
  type ReasoningEffort,
} from "./detail/reasoning-effort";
import { joinInstructions } from "./detail/sections";

/**
 * A new assistant starts from a name, a model, and what it should do. Tools,
 * Slack, and schedules are added on its page once it exists.
 */
export default function NewAssistantPage(): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [model, setModel] = useState<string>(DEFAULT_ASSISTANT_MODEL);
  const [reasoningEffort, setReasoningEffort] =
    useState<ReasoningEffort>("medium");
  const [tasks, setTasks] = useState("");
  const [preset, setPreset] = useState<string>();
  const [personality, setPersonality] = useState("");

  const create = useAssistantsCreateMutation({
    onSuccess: (assistant) => {
      void invalidateAllAssistantsList(queryClient);
      routes.assistants.detail.goTo(assistant.id);
    },
  });

  const canSubmit = name.trim() !== "" && tasks.trim() !== "";

  return (
    <FormPage
      scope="assistant:write"
      resourceId={project.id}
      title="New assistant"
      description="Add tools and Slack after creating it."
      stage="beta"
    >
      <form
        className="flex flex-col gap-6"
        onSubmit={(e) => {
          e.preventDefault();
          if (!canSubmit || create.isPending) return;
          create.mutate({
            request: {
              createAssistantForm: {
                name: name.trim(),
                model,
                reasoningEffort,
                toolsets: [],
                instructions: joinInstructions({
                  preamble: "",
                  Personality: personality,
                  Tasks: tasks,
                  Behavior: computeBehaviorSection([]),
                }),
              },
            },
          });
        }}
      >
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="assistant-name">Name</Label>
          <Input
            id="assistant-name"
            value={name}
            onChange={setName}
            maxLength={120}
            placeholder="e.g. Support triage"
            autoFocus
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="assistant-tasks">What it does</Label>
          <TextArea
            id="assistant-tasks"
            value={tasks}
            onChange={setTasks}
            rows={6}
            placeholder="e.g. Triage new bug reports in #support."
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <div className="flex items-end justify-between gap-4">
            <Label htmlFor="assistant-personality">
              Personality (optional)
            </Label>
            <PersonalitySelect
              value={preset}
              className="w-56"
              onPick={(p) => {
                setPreset(p.slug);
                setPersonality(p.instructions);
              }}
            />
          </div>
          <TextArea
            id="assistant-personality"
            value={personality}
            onChange={(value) => {
              setPreset(undefined);
              setPersonality(value);
            }}
            rows={5}
          />
        </div>

        <div className="grid gap-6 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label>Model</Label>
            <ModelSelect value={model} onValueChange={setModel} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>Reasoning effort</Label>
            <ReasoningEffortSelect
              value={reasoningEffort}
              onValueChange={setReasoningEffort}
            />
            <Text small muted>
              {REASONING_EFFORT_HELP}
            </Text>
          </div>
        </div>

        {create.error ? (
          <Text small className="text-destructive">
            Couldn't create the assistant. Try again.
          </Text>
        ) : null}

        <div className="flex justify-end gap-2">
          <Button
            type="button"
            variant="tertiary"
            onClick={() => routes.assistants.goTo()}
          >
            Cancel
          </Button>
          <Button type="submit" disabled={!canSubmit || create.isPending}>
            {create.isPending ? "Creating…" : "Create assistant"}
          </Button>
        </div>
      </form>
    </FormPage>
  );
}
