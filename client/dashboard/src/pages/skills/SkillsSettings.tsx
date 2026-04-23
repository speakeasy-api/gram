import { RequireScope } from "@/components/require-scope";
import { Skeleton } from "@/components/ui/skeleton";
import { Heading } from "@/components/ui/heading";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import type {
  SetSkillCaptureSettingsForm,
  SkillCaptureSettings,
} from "@gram/client/models/components";
import {
  invalidateAllSkillsSettings,
  useSetSkillsSettingsMutation,
  useSkillsSettings,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { FolderTree, Sparkles, UserRound } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { toast } from "sonner";

export default function SkillsSettings() {
  return (
    <div className="p-8">
      <div className="mx-auto max-w-4xl">
        <RequireScope scope="project:read" level="page">
          <SkillsSettingsInner />
        </RequireScope>
      </div>
    </div>
  );
}

function SkillsSettingsInner() {
  const queryClient = useQueryClient();
  const { data, isPending, error } = useSkillsSettings();
  const [settings, setSettings] = useState<SkillCaptureSettings | null>(null);

  useEffect(() => {
    if (data) {
      setSettings(data);
    }
  }, [data]);

  const currentSettings = settings ?? data ?? null;

  const mutation = useSetSkillsSettingsMutation({
    onSuccess: async (nextSettings) => {
      setSettings(nextSettings);
      await invalidateAllSkillsSettings(queryClient);
      toast.success("Capture settings updated");
    },
    onError: () => {
      toast.error("Failed to update capture settings");
    },
  });

  const handleUpdate = (next: SetSkillCaptureSettingsForm) => {
    mutation.mutate({
      request: {
        setSkillCaptureSettingsForm: next,
      },
    });
  };

  return (
    <div className="space-y-6">
      <div>
        <Type variant="subheading">Settings</Type>
        <Type small muted className="mt-1 block max-w-2xl">
          Configure how hook-based skill captures are accepted for this project.
        </Type>
      </div>

      {error ? (
        <SettingsErrorState />
      ) : isPending && !currentSettings ? (
        <SettingsSkeleton />
      ) : currentSettings ? (
        <section className="border-border bg-card rounded-xl border p-5">
          <Stack gap={4}>
            <div className="flex items-start justify-between gap-4">
              <div>
                <div className="flex items-center gap-2">
                  <Sparkles className="text-muted-foreground h-4 w-4" />
                  <Heading variant="h5">Capture</Heading>
                </div>
                <Type muted small className="mt-1">
                  Skill capture writes a project-level override using the
                  existing capture policy model.
                </Type>
              </div>
              <ModePill mode={currentSettings.effectiveMode} />
            </div>

            <div className="border-border border-t" />

            <CaptureToggleRow
              icon={<Sparkles className="text-muted-foreground h-4 w-4" />}
              label="Enable Capture"
              description="Allow this project to accept captured skills from configured hooks."
              checked={currentSettings.enabled}
              disabled={mutation.isPending}
              onCheckedChange={(checked) =>
                handleUpdate(getEnabledSettings(currentSettings, checked))
              }
            />

            <div className="border-border border-t" />

            <CaptureToggleRow
              icon={<FolderTree className="text-muted-foreground h-4 w-4" />}
              label="Capture Project Skills"
              description="Accept skills discovered from project-local locations."
              checked={currentSettings.captureProjectSkills}
              disabled={mutation.isPending}
              onCheckedChange={(checked) =>
                handleUpdate(
                  getScopedSettings(currentSettings, "project", checked),
                )
              }
            />

            <div className="border-border border-t" />

            <CaptureToggleRow
              icon={<UserRound className="text-muted-foreground h-4 w-4" />}
              label="Capture User Skills"
              description="Accept skills discovered from user-local locations."
              checked={currentSettings.captureUserSkills}
              disabled={mutation.isPending}
              onCheckedChange={(checked) =>
                handleUpdate(
                  getScopedSettings(currentSettings, "user", checked),
                )
              }
            />
          </Stack>
        </section>
      ) : null}
    </div>
  );
}

function CaptureToggleRow({
  icon,
  label,
  description,
  checked,
  disabled,
  onCheckedChange,
}: {
  icon: ReactNode;
  label: string;
  description: string;
  checked: boolean;
  disabled: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          {icon}
          <Type variant="body" className="font-medium">
            {label}
          </Type>
        </div>
        <Type variant="body" className="text-muted-foreground ml-6 text-sm">
          {description}
        </Type>
      </div>
      <RequireScope
        scope="project:write"
        level="component"
        reason="You need project:write to update capture settings."
      >
        <Switch
          checked={checked}
          disabled={disabled}
          onCheckedChange={onCheckedChange}
          aria-label={label}
        />
      </RequireScope>
    </div>
  );
}

function ModePill({ mode }: { mode: SkillCaptureSettings["effectiveMode"] }) {
  return (
    <div className="bg-muted text-muted-foreground rounded-full px-3 py-1 text-xs font-medium">
      {formatModeLabel(mode)}
    </div>
  );
}

function SettingsSkeleton() {
  return (
    <div className="border-border bg-card rounded-xl border p-5">
      <div className="space-y-4">
        <div className="flex items-center justify-between gap-4">
          <div className="space-y-2">
            <Skeleton className="h-5 w-24" />
            <Skeleton className="h-4 w-80" />
          </div>
          <Skeleton className="h-7 w-28 rounded-full" />
        </div>
        <div className="space-y-4 pt-2">
          {Array.from({ length: 3 }).map((_, index) => (
            <div
              key={index}
              className="flex items-center justify-between gap-4"
            >
              <div className="space-y-2">
                <Skeleton className="h-4 w-44" />
                <Skeleton className="h-4 w-72" />
              </div>
              <Skeleton className="h-6 w-10 rounded-full" />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function SettingsErrorState() {
  return (
    <div className="rounded-xl border border-dashed px-8 py-16 text-center">
      <Type variant="subheading" className="mb-1">
        Couldn&apos;t load capture settings
      </Type>
      <Type small muted>
        The settings surface is wired, but the project capture policy could not
        be loaded.
      </Type>
    </div>
  );
}

function getEnabledSettings(
  settings: SkillCaptureSettings,
  checked: boolean,
): SetSkillCaptureSettingsForm {
  if (!checked) {
    return {
      enabled: false,
      captureProjectSkills: false,
      captureUserSkills: false,
    };
  }

  const captureProjectSkills =
    settings.captureProjectSkills || !settings.captureUserSkills;

  return {
    enabled: true,
    captureProjectSkills,
    captureUserSkills: settings.captureUserSkills,
  };
}

function getScopedSettings(
  settings: SkillCaptureSettings,
  scope: "project" | "user",
  checked: boolean,
): SetSkillCaptureSettingsForm {
  const next = {
    captureProjectSkills:
      scope === "project" ? checked : settings.captureProjectSkills,
    captureUserSkills: scope === "user" ? checked : settings.captureUserSkills,
  };

  return {
    enabled: next.captureProjectSkills || next.captureUserSkills,
    captureProjectSkills: next.captureProjectSkills,
    captureUserSkills: next.captureUserSkills,
  };
}

function formatModeLabel(mode: SkillCaptureSettings["effectiveMode"]) {
  switch (mode) {
    case "project_and_user":
      return "Project + user";
    case "project_only":
      return "Project only";
    case "user_only":
      return "User only";
    default:
      return "Disabled";
  }
}
