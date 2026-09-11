import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type JSX } from "react";
import type { AdminOnboardingConfiguration } from "@gram/admin-client/models/components/adminonboardingconfiguration";
import type { SetOrganizationOnboardingRequestBody } from "@gram/admin-client/models/components/setorganizationonboardingrequestbody";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  organizationOnboardingQuery,
  setAdminOrganizationOnboarding,
} from "@/lib/gramAdminClient";

function selection(
  data: AdminOnboardingConfiguration,
): SetOrganizationOnboardingRequestBody {
  return {
    organizationId: data.organizationId,
    preset: data.preset,
    visibleTaskKeys: data.tasks
      .filter((task) => !task.hidden)
      .map((task) => task.key),
  };
}

export function Onboarding({
  organizationId,
}: {
  organizationId: string;
}): JSX.Element {
  return (
    <OnboardingEditor key={organizationId} organizationId={organizationId} />
  );
}

function OnboardingEditor({
  organizationId,
}: {
  organizationId: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const query = organizationOnboardingQuery(organizationId);
  const { data, isPending, isError, refetch } = useQuery({
    ...query,
    throwOnError: false,
  });
  const [draft, setDraft] =
    useState<SetOrganizationOnboardingRequestBody | null>(null);
  const [presetChoice, setPresetChoice] = useState<string>("");
  const [confirm, dialog] = useConfirmDialog();
  const mutation = useMutation({
    mutationFn: setAdminOrganizationOnboarding,
    onSuccess: async (updated) => {
      await queryClient.cancelQueries({ queryKey: query.queryKey });
      queryClient.setQueryData(query.queryKey, updated);
      setDraft(null);
      setPresetChoice("");
      await queryClient.invalidateQueries({ queryKey: query.queryKey });
    },
  });

  if (isPending) return <p role="status">Loading onboarding...</p>;
  if (!data)
    return (
      <div role="alert">
        <p>Unable to load onboarding configuration.</p>
        <Button variant="outline" onClick={() => void refetch()}>
          Retry onboarding
        </Button>
      </div>
    );

  const current = draft ?? selection(data);
  const selectedPreset = data.presets.find(
    (preset) => preset.key === current.preset,
  );
  const customized =
    selectedPreset &&
    (selectedPreset.visibleTaskKeys.length !== current.visibleTaskKeys.length ||
      selectedPreset.visibleTaskKeys.some(
        (key) => !current.visibleTaskKeys.includes(key),
      ));
  const choice = presetChoice || current.preset || "";

  return (
    <section
      className="border-border overflow-hidden rounded-md border"
      aria-labelledby="onboarding-heading"
    >
      <div className="border-border bg-muted/20 border-b px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <h5 id="onboarding-heading" className="text-sm font-medium">
            Onboarding
          </h5>
          <Badge variant="outline">
            {current.preset ?? "Legacy"}
            {customized ? " - customized" : ""}
          </Badge>
          {draft && <Badge variant="secondary">Unsaved changes</Badge>}
        </div>
        <p className="text-muted-foreground text-sm">
          Choose customer onboarding tasks. This does not change entitlements,
          permissions, assignments, or progress.
        </p>
      </div>
      <div className="space-y-4 p-4">
        {isError && (
          <p role="alert">
            Unable to refresh onboarding; showing the last loaded configuration.
          </p>
        )}
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={choice}
            onValueChange={setPresetChoice}
            disabled={mutation.isPending}
          >
            <SelectTrigger aria-label="Onboarding preset">
              <SelectValue placeholder="Choose a preset" />
            </SelectTrigger>
            <SelectContent>
              {data.presets.map((preset) => (
                <SelectItem key={preset.key} value={preset.key}>
                  {preset.key === "gateway" ? "Gateway" : "Security"}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            disabled={!choice || mutation.isPending}
            onClick={() => {
              const preset = data.presets.find((item) => item.key === choice);
              if (!preset) return;
              void confirm({
                title: "Replace task selection?",
                description:
                  "Applying this preset replaces your task customizations, including unsaved changes. Existing progress and assignments are kept. Nothing changes for the organization until you save.",
                confirmLabel: "Apply to draft",
              }).then((confirmed) => {
                if (!confirmed) return;
                mutation.reset();
                setDraft({
                  organizationId,
                  preset: preset.key,
                  visibleTaskKeys: [...preset.visibleTaskKeys],
                });
              });
            }}
          >
            Apply preset
          </Button>
        </div>
        <div className="space-y-3">
          {data.tasks.map((task) => (
            <label
              key={task.key}
              className="flex cursor-pointer items-start gap-3"
            >
              <Checkbox
                aria-label={task.title}
                checked={current.visibleTaskKeys.includes(task.key)}
                disabled={mutation.isPending}
                onCheckedChange={(checked) => {
                  mutation.reset();
                  setPresetChoice("");
                  setDraft({
                    ...current,
                    visibleTaskKeys:
                      checked === true
                        ? [...current.visibleTaskKeys, task.key]
                        : current.visibleTaskKeys.filter(
                            (key) => key !== task.key,
                          ),
                  });
                }}
              />
              <span>
                <span className="block text-sm font-medium">{task.title}</span>
                <span className="text-muted-foreground text-sm">
                  {task.description}
                </span>
              </span>
            </label>
          ))}
        </div>
        <p className="text-muted-foreground text-sm">
          {current.visibleTaskKeys.length} of {data.tasks.length} tasks
          selected.
          {current.visibleTaskKeys.length === 0
            ? " Customers will see no onboarding tasks."
            : ""}
        </p>
        {mutation.isError && (
          <p role="alert" className="text-destructive text-sm">
            Unable to save onboarding: {errorMessage(mutation.error)}. Your
            draft is kept; retry Save.
          </p>
        )}
        {mutation.isSuccess && !draft && <p role="status">Onboarding saved.</p>}
        <div className="flex gap-2">
          <Button
            disabled={!draft || mutation.isPending}
            onClick={() => mutation.mutate(current)}
          >
            {mutation.isPending ? "Saving..." : "Save onboarding"}
          </Button>
          <Button
            variant="ghost"
            disabled={!draft || mutation.isPending}
            onClick={() => {
              setDraft(null);
              setPresetChoice("");
              mutation.reset();
            }}
          >
            Discard changes
          </Button>
        </div>
      </div>
      {dialog}
    </section>
  );
}
