import { ErrorAlert } from "@/components/ui/alert";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { useRBAC } from "@/hooks/useRBAC";
import type { SkillEfficacySettings } from "@gram/client/models/components/skillefficacysettings.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import {
  invalidateAllSkillEfficacySettings,
  useSkillEfficacySettings,
} from "@gram/client/react-query/skillEfficacySettings.js";
import { useUpsertSkillEfficacySettingsMutation } from "@gram/client/react-query/upsertSkillEfficacySettings.js";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";

const MIN_LIMIT = 0;
const MAX_LIMIT = 10_000;

type LimitFieldProps = {
  id: string;
  label: string;
  description: string;
  defaultValue: number;
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
};

function limitError(value: string): string | undefined {
  if (value.trim() === "") return "Enter a value.";
  const parsed = Number(value);
  if (!Number.isInteger(parsed)) return "Enter a whole number.";
  if (parsed < MIN_LIMIT || parsed > MAX_LIMIT) {
    return `Enter a value from ${MIN_LIMIT.toLocaleString()} to ${MAX_LIMIT.toLocaleString()}.`;
  }
  return undefined;
}

function LimitField({
  id,
  label,
  description,
  defaultValue,
  value,
  disabled,
  onChange,
}: LimitFieldProps): JSX.Element {
  const error = limitError(value);
  return (
    <Field data-invalid={error ? true : undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        min={MIN_LIMIT}
        max={MAX_LIMIT}
        step={1}
        value={value}
        onChange={onChange}
        disabled={disabled}
        aria-invalid={error ? true : undefined}
        className="max-w-40"
      />
      <FieldDescription>
        {description} Default: {defaultValue.toLocaleString()}.
      </FieldDescription>
      <FieldError>{error}</FieldError>
    </Field>
  );
}

export function SkillEfficacySettingsSection(): JSX.Element | null {
  const { hasScope, isLoading: isRBACLoading } = useRBAC();
  const { data: features, isLoading: isFeaturesLoading } = useProductFeatures(
    undefined,
    undefined,
    { staleTime: 30_000, throwOnError: false },
  );

  if (isRBACLoading || isFeaturesLoading) return null;
  if (!hasScope("org:admin") || features?.skillsEnabled !== true) return null;

  return <SkillEfficacySettingsQuery />;
}

function SkillEfficacySettingsQuery(): JSX.Element {
  const query = useSkillEfficacySettings(undefined, undefined, {
    throwOnError: false,
  });

  if (query.isLoading || !query.data) {
    if (query.error) {
      return (
        <SettingsShell>
          <ErrorAlert
            title="Unable to load skill efficacy settings"
            error={query.error}
            className="max-w-2xl"
          />
          <Button variant="secondary" onClick={() => void query.refetch()}>
            Try again
          </Button>
        </SettingsShell>
      );
    }
    return (
      <SettingsShell>
        <Skeleton className="h-56 w-full max-w-2xl" />
      </SettingsShell>
    );
  }

  const settings = query.data;
  const formKey = [
    settings.enabled,
    settings.perSkillDailyCap,
    settings.orgDailyCap,
    settings.newVersionBurst,
  ].join(":");

  return (
    <SettingsShell>
      <SkillEfficacySettingsForm key={formKey} settings={settings} />
    </SettingsShell>
  );
}

function SettingsShell({ children }: { children: ReactNode }): JSX.Element {
  return (
    <section className="mt-8">
      <Heading variant="h4" className="mb-2">
        Skill efficacy sampling
      </Heading>
      <Type muted small className="mb-6 max-w-2xl">
        Control how many skill sessions are scored across the organization.
      </Type>
      {children}
    </section>
  );
}

function SkillEfficacySettingsForm({
  settings,
}: {
  settings: SkillEfficacySettings;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [enabled, setEnabled] = useState(settings.enabled);
  const [perSkillDailyCap, setPerSkillDailyCap] = useState(
    String(settings.perSkillDailyCap),
  );
  const [orgDailyCap, setOrgDailyCap] = useState(String(settings.orgDailyCap));
  const [newVersionBurst, setNewVersionBurst] = useState(
    String(settings.newVersionBurst),
  );
  const [saveError, setSaveError] = useState<string>();

  const mutation = useUpsertSkillEfficacySettingsMutation({
    onSuccess: async () => {
      setSaveError(undefined);
      await invalidateAllSkillEfficacySettings(queryClient);
      toast.success("Skill efficacy sampling settings saved");
    },
    onError: (error) => {
      setSaveError(error.message);
      toast.error("Failed to save skill efficacy sampling settings");
    },
  });

  const hasValidationError = [
    perSkillDailyCap,
    orgDailyCap,
    newVersionBurst,
  ].some((value) => limitError(value) !== undefined);
  const isDirty =
    enabled !== settings.enabled ||
    perSkillDailyCap !== String(settings.perSkillDailyCap) ||
    orgDailyCap !== String(settings.orgDailyCap) ||
    newVersionBurst !== String(settings.newVersionBurst);
  const disabled = mutation.isPending;

  const handleSave = () => {
    if (hasValidationError) return;
    setSaveError(undefined);
    mutation.mutate({
      request: {
        upsertSettingsRequestBody: {
          enabled,
          perSkillDailyCap: Number(perSkillDailyCap),
          orgDailyCap: Number(orgDailyCap),
          newVersionBurst: Number(newVersionBurst),
        },
      },
    });
  };

  return (
    <div className="border-border bg-card max-w-2xl rounded-lg border p-6">
      <Stack gap={6}>
        <Stack direction="horizontal" justify="space-between" align="center">
          <div>
            <Type variant="body" className="font-medium">
              Enable scoring
            </Type>
            <Type muted small>
              Sample completed skill sessions for efficacy insights.
            </Type>
          </div>
          <Switch
            checked={enabled}
            onCheckedChange={setEnabled}
            disabled={disabled}
            aria-label="Enable skill efficacy scoring"
          />
        </Stack>

        <div className="border-border border-t" />

        <div className="grid gap-6 sm:grid-cols-3">
          <LimitField
            id="skill-efficacy-per-skill-cap"
            label="Per-skill daily cap"
            description="Maximum evaluations reserved per skill each UTC day."
            defaultValue={10}
            value={perSkillDailyCap}
            disabled={disabled}
            onChange={setPerSkillDailyCap}
          />
          <LimitField
            id="skill-efficacy-org-cap"
            label="Organization daily ceiling"
            description="Maximum evaluations reserved across the organization each UTC day."
            defaultValue={100}
            value={orgDailyCap}
            disabled={disabled}
            onChange={setOrgDailyCap}
          />
          <LimitField
            id="skill-efficacy-version-burst"
            label="New-version lifetime burst"
            description="Evaluations available to each new skill version before its daily cap applies."
            defaultValue={25}
            value={newVersionBurst}
            disabled={disabled}
            onChange={setNewVersionBurst}
          />
        </div>

        <div className="bg-muted/40 rounded-md border p-4">
          <Type muted small>
            Daily caps reset at 00:00 UTC. The new-version burst bypasses the
            per-skill daily cap until exhausted, but it never bypasses the
            organization daily ceiling.
          </Type>
        </div>

        {settings.isDefault && (
          <Type muted small>
            These are the current platform defaults. Saving creates settings for
            this organization.
          </Type>
        )}

        {saveError && (
          <ErrorAlert
            title="Unable to save skill efficacy settings"
            error={saveError}
            onDismiss={() => setSaveError(undefined)}
          />
        )}

        <Button
          variant="primary"
          onClick={handleSave}
          disabled={!isDirty || hasValidationError || disabled}
        >
          {mutation.isPending ? "Saving..." : "Save settings"}
        </Button>
      </Stack>
    </div>
  );
}
