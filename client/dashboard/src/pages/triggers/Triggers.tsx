import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Type } from "@/components/ui/type";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { HumanizeDateTime } from "@/lib/dates";
import {
  useTriggers,
  useTriggerDefinitions,
  useCreateTriggerMutation,
  useUpdateTriggerMutation,
  useDeleteTriggerMutation,
  invalidateAllTriggers,
  invalidateAllTrigger,
  useListEnvironments,
} from "@gram/client/react-query";
import { TriggerInstance } from "@gram/client/models/components/triggerinstance.js";
import { TriggerDefinition } from "@gram/client/models/components/triggerdefinition.js";
import { TargetKind } from "@gram/client/models/components/createtriggerinstanceform.js";
import { useRoutes } from "@/routes";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Outlet } from "react-router";

export function TriggersRoot() {
  return <Outlet />;
}

type TriggerConfig = Record<string, unknown>;

type TriggerConfigSchemaProperty = {
  default?: string;
  description?: string;
  items?: {
    enum?: string[];
  };
  title?: string;
  type?: string | string[];
};

type TriggerConfigSchema = {
  properties?: Record<string, TriggerConfigSchemaProperty>;
  required?: string[];
};

type TriggerTargetKindValue = (typeof TargetKind)[keyof typeof TargetKind];

const triggerTargetKindOptions: Array<{
  value: TriggerTargetKindValue;
  label: string;
  description: string;
}> = [
  {
    value: TargetKind.Assistant,
    label: "Assistant",
    description: "Dispatch the trigger to an assistant target.",
  },
  {
    value: TargetKind.Noop,
    label: "Noop sink",
    description:
      "Test sink that records dispatches without executing anything.",
  },
];

function isTriggerTargetKind(value: string): value is TriggerTargetKindValue {
  return triggerTargetKindOptions.some((option) => option.value === value);
}

function StatusBadge({ status }: { status: string }) {
  if (status === "active") {
    return <Badge variant="default">Active</Badge>;
  }
  return <Badge variant="secondary">Paused</Badge>;
}

function KindBadge({ kind }: { kind: string }) {
  if (kind === "webhook") {
    return <Badge variant="outline">Webhook</Badge>;
  }
  return <Badge variant="outline">Schedule</Badge>;
}

function WebhookUrlPill({ url }: { url: string }) {
  const [copied, setCopied] = useState(false);
  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    e.preventDefault();
    navigator.clipboard.writeText(url);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <button
      type="button"
      onClick={handleClick}
      title={copied ? "Copied!" : url}
      className="group border-border bg-muted/30 hover:border-muted-foreground/40 hover:bg-muted/50 focus-visible:ring-ring inline-flex max-w-[260px] min-w-0 items-center gap-2 rounded-full border px-2.5 py-0.5 text-xs transition-colors focus-visible:ring-2 focus-visible:outline-none"
    >
      <span className="text-foreground shrink-0 font-semibold tracking-wide uppercase">
        URL
      </span>
      <span aria-hidden="true" className="bg-border h-3 w-px shrink-0" />
      <span className="text-muted-foreground truncate font-mono">{url}</span>
      <Icon
        name={copied ? "check" : "copy"}
        className="text-muted-foreground group-hover:text-foreground h-3 w-3 shrink-0"
      />
    </button>
  );
}

function TriggersEmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon name="zap" className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No triggers yet
      </Type>
      <Type small muted className="mb-4 max-w-md text-center">
        Triggers let you connect external events to your assistants. Set up a
        cron schedule or a webhook to get started.
      </Type>
      <Button onClick={onCreate}>
        <Button.LeftIcon>
          <Icon name="plus" className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Create Trigger</Button.Text>
      </Button>
    </div>
  );
}

function triggerLogsFilterParam(triggerId: string): string {
  return `${encodeURIComponent("gram.trigger.instance_id")}:eq:${encodeURIComponent(triggerId)}`;
}

function TriggersTable({
  triggers,
  definitions,
  onEdit,
}: {
  triggers: TriggerInstance[];
  definitions: TriggerDefinition[];
  onEdit: (trigger: TriggerInstance) => void;
}) {
  const routes = useRoutes();
  const defMap = new Map(definitions.map((d) => [d.slug, d]));

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Type</TableHead>
          <TableHead>Target</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Updated</TableHead>
          <TableHead className="w-10" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {triggers.map((trigger) => {
          const def = defMap.get(trigger.definitionSlug);
          return (
            <TableRow
              key={trigger.id}
              className="cursor-pointer"
              onClick={() => onEdit(trigger)}
            >
              <TableCell className="font-medium">{trigger.name}</TableCell>
              <TableCell>
                <div className="flex min-w-0 items-center gap-2">
                  <KindBadge kind={def?.kind ?? "webhook"} />
                  <span className="text-muted-foreground shrink-0 text-sm">
                    {def?.title ?? trigger.definitionSlug}
                  </span>
                  {def?.kind === "webhook" && trigger.webhookUrl && (
                    <WebhookUrlPill url={trigger.webhookUrl} />
                  )}
                </div>
              </TableCell>
              <TableCell className="text-muted-foreground">
                {trigger.targetDisplay}
              </TableCell>
              <TableCell>
                <StatusBadge status={trigger.status} />
              </TableCell>
              <TableCell className="text-muted-foreground">
                <HumanizeDateTime date={trigger.updatedAt} />
              </TableCell>
              <TableCell>
                <div onClick={(e) => e.stopPropagation()}>
                  <routes.logs.Link
                    queryParams={{ af: triggerLogsFilterParam(trigger.id) }}
                    className="text-muted-foreground hover:text-foreground no-underline hover:no-underline"
                  >
                    <Icon name="file-text" className="h-4 w-4" />
                  </routes.logs.Link>
                </div>
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

function ConfigField({
  fieldKey,
  prop,
  isRequired,
  config,
  onChange,
}: {
  fieldKey: string;
  prop: TriggerConfigSchemaProperty;
  isRequired: boolean;
  config: TriggerConfig;
  onChange: (config: TriggerConfig) => void;
}) {
  const label = prop.title || fieldKey;
  const description = prop.description;

  const isArrayType =
    prop.type === "array" ||
    (Array.isArray(prop.type) && prop.type.includes("array"));

  if (isArrayType && prop.items?.enum) {
    const options: string[] = prop.items.enum;
    const rawSelected = config[fieldKey];
    const selected = Array.isArray(rawSelected)
      ? rawSelected.filter(
          (value): value is string => typeof value === "string",
        )
      : [];
    const toggle = (val: string) => {
      const next = selected.includes(val)
        ? selected.filter((v: string) => v !== val)
        : [...selected, val];
      onChange({ ...config, [fieldKey]: next });
    };
    return (
      <div>
        <Type variant="body" className="mb-1 font-medium">
          {label}
          {isRequired && " *"}
        </Type>
        {description && (
          <Type small muted className="mb-2">
            {description}
          </Type>
        )}
        <div className="flex flex-wrap gap-2">
          {options.map((opt) => (
            <button
              key={opt}
              type="button"
              onClick={() => toggle(opt)}
              className={
                selected.includes(opt)
                  ? "border-primary bg-primary/5 rounded-md border px-3 py-1 text-sm"
                  : "border-border hover:border-muted-foreground/30 rounded-md border px-3 py-1 text-sm"
              }
            >
              {opt}
            </button>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div>
      <Type variant="body" className="mb-1 font-medium">
        {label}
        {isRequired && " *"}
      </Type>
      {description && (
        <Type small muted className="mb-1">
          {description}
        </Type>
      )}
      <Input
        value={typeof config[fieldKey] === "string" ? config[fieldKey] : ""}
        onChange={(val) => onChange({ ...config, [fieldKey]: val })}
        placeholder={typeof prop.default === "string" ? prop.default : ""}
      />
    </div>
  );
}

function TriggerConfigFields({
  definition,
  config,
  onChange,
}: {
  definition: TriggerDefinition;
  config: TriggerConfig;
  onChange: (config: TriggerConfig) => void;
}) {
  let schema: TriggerConfigSchema;
  try {
    schema = JSON.parse(definition.configSchema) as TriggerConfigSchema;
  } catch {
    return (
      <Type small muted>
        Unable to parse config schema.
      </Type>
    );
  }

  const properties = schema.properties ?? {};
  const required: string[] = schema.required ?? [];

  const requiredEntries = Object.entries(properties).filter(([key]) =>
    required.includes(key),
  );
  const optionalEntries = Object.entries(properties).filter(
    ([key]) => !required.includes(key),
  );

  return (
    <Stack gap={3}>
      {requiredEntries.map(([key, prop]) => (
        <ConfigField
          key={key}
          fieldKey={key}
          prop={prop}
          isRequired
          config={config}
          onChange={onChange}
        />
      ))}
      {optionalEntries.length > 0 && (
        <Collapsible>
          <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm transition-colors [&[data-state=open]>svg]:rotate-90">
            <Icon
              name="chevron-right"
              className="h-4 w-4 transition-transform"
            />
            Advanced
          </CollapsibleTrigger>
          <CollapsibleContent>
            <Stack gap={3} className="pt-3">
              {optionalEntries.map(([key, prop]) => (
                <ConfigField
                  key={key}
                  fieldKey={key}
                  prop={prop}
                  isRequired={false}
                  config={config}
                  onChange={onChange}
                />
              ))}
            </Stack>
          </CollapsibleContent>
        </Collapsible>
      )}
    </Stack>
  );
}

function TriggerDialog({
  open,
  onOpenChange,
  editingTrigger,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editingTrigger: TriggerInstance | null;
}) {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const { data: definitionsData } = useTriggerDefinitions(undefined, {
    retry: false,
    throwOnError: false,
  });
  const createMutation = useCreateTriggerMutation();
  const updateMutation = useUpdateTriggerMutation();
  const deleteMutation = useDeleteTriggerMutation();

  const definitions = definitionsData?.definitions ?? [];

  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];

  const [name, setName] = useState("");
  const [definitionSlug, setDefinitionSlug] = useState("");
  const [environmentId, setEnvironmentId] = useState("");
  const [config, setConfig] = useState<TriggerConfig>({});
  const [targetKind, setTargetKind] = useState<TriggerTargetKindValue>();
  const [targetDisplay, setTargetDisplay] = useState("");
  const [targetRef, setTargetRef] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);

  const isEditing = editingTrigger !== null;
  const selectedDefinition = definitions.find((d) => d.slug === definitionSlug);

  const populateFromTrigger = (trigger: TriggerInstance) => {
    setName(trigger.name);
    setDefinitionSlug(trigger.definitionSlug);
    setEnvironmentId(trigger.environmentId ?? "");
    setConfig(trigger.config);
    setTargetKind(
      isTriggerTargetKind(trigger.targetKind) ? trigger.targetKind : undefined,
    );
    setTargetDisplay(trigger.targetDisplay);
    setTargetRef(trigger.targetRef);
    setConfirmDelete(false);
  };

  const reset = () => {
    setName("");
    setDefinitionSlug("");
    setEnvironmentId("");
    setConfig({});
    setTargetKind(undefined);
    setTargetDisplay("");
    setTargetRef("");
    setConfirmDelete(false);
  };

  const handleOpenChange = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  // Populate form when editing
  const prevEditId = useState<string | null>(null);
  if (editingTrigger && editingTrigger.id !== prevEditId[0]) {
    prevEditId[1](editingTrigger.id);
    populateFromTrigger(editingTrigger);
  }
  if (!editingTrigger && prevEditId[0] !== null) {
    prevEditId[1](null);
  }

  const needsEnvironment =
    selectedDefinition != null &&
    selectedDefinition.envRequirements.some((r) => r.required);

  const selectedEnvironment = environments.find((e) => e.id === environmentId);
  const missingEnvVars =
    selectedDefinition && selectedEnvironment
      ? selectedDefinition.envRequirements
          .filter((r) => r.required)
          .filter(
            (r) => !selectedEnvironment.entries.some((e) => e.name === r.name),
          )
      : [];

  const isValid =
    name.trim().length > 0 &&
    definitionSlug.length > 0 &&
    (!needsEnvironment || environmentId.length > 0) &&
    targetKind != null &&
    targetRef.trim().length > 0 &&
    targetDisplay.trim().length > 0;

  const invalidateAll = () => {
    invalidateAllTriggers(queryClient);
    invalidateAllTrigger(queryClient);
  };

  const handleCreate = async () => {
    if (!targetKind) return;

    await createMutation.mutateAsync({
      request: {
        name,
        definitionSlug,
        config,
        targetKind,
        targetRef,
        targetDisplay,
        ...(environmentId ? { environmentId } : {}),
      },
    });
    invalidateAll();
    handleOpenChange(false);
  };

  const handleUpdate = async () => {
    if (!editingTrigger || !targetKind) return;
    await updateMutation.mutateAsync({
      request: {
        id: editingTrigger.id,
        name,
        config,
        targetKind,
        targetRef,
        targetDisplay,
        ...(environmentId ? { environmentId } : {}),
      },
    });
    invalidateAll();
    handleOpenChange(false);
  };

  const handleDelete = async () => {
    if (!editingTrigger) return;
    await deleteMutation.mutateAsync({
      request: { id: editingTrigger.id },
    });
    invalidateAll();
    handleOpenChange(false);
  };

  const isPending =
    createMutation.isPending ||
    updateMutation.isPending ||
    deleteMutation.isPending;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content className="flex max-h-[85vh] flex-col sm:max-w-xl">
        <Dialog.Header>
          <Dialog.Title>
            {isEditing ? "Edit Trigger" : "Create Trigger"}
          </Dialog.Title>
          <Dialog.Description>
            {isEditing
              ? "Update the trigger configuration."
              : "Choose a trigger type and configure it."}
          </Dialog.Description>
        </Dialog.Header>

        <div className="min-h-0 overflow-y-auto">
          <Stack gap={4}>
            <div>
              <Type variant="body" className="mb-1 font-medium">
                Name
              </Type>
              <Input value={name} onChange={setName} placeholder="My Trigger" />
            </div>

            {!isEditing && (
              <div>
                <Type variant="body" className="mb-1 font-medium">
                  Trigger Type
                </Type>
                <Select
                  value={definitionSlug}
                  onValueChange={(val) => {
                    setDefinitionSlug(val);
                    setConfig({});
                  }}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="Select a trigger type..." />
                  </SelectTrigger>
                  <SelectContent>
                    {definitions.map((def) => (
                      <SelectItem
                        key={def.slug}
                        value={def.slug}
                        description={def.description}
                      >
                        {def.title}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            {needsEnvironment && (
              <div>
                <Type variant="body" className="mb-1 font-medium">
                  Environment
                </Type>
                <Type small muted className="mb-1">
                  This trigger type requires environment variables. Select an
                  environment that has them configured.
                </Type>
                <Select value={environmentId} onValueChange={setEnvironmentId}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="Select an environment..." />
                  </SelectTrigger>
                  <SelectContent>
                    {environments.map((env) => (
                      <SelectItem key={env.id} value={env.id}>
                        {env.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            {missingEnvVars.length > 0 && selectedEnvironment && (
              <div className="border-warning-default bg-warning-softest rounded-md border p-3">
                <Type variant="body" className="mb-1 font-medium">
                  Missing environment variables
                </Type>
                <Type small className="text-warning-foreground">
                  The selected environment is missing required variables. The
                  trigger will be created but will fail at runtime until these
                  are configured in{" "}
                  <routes.environments.environment.Link
                    params={[selectedEnvironment.slug]}
                    className="font-medium underline"
                  >
                    {selectedEnvironment.name}
                  </routes.environments.environment.Link>
                  :
                </Type>
                <ul className="mt-2 space-y-1">
                  {missingEnvVars.map((req) => (
                    <li
                      key={req.name}
                      className="flex items-center gap-2 text-sm"
                    >
                      <code className="bg-muted rounded px-1.5 py-0.5 text-xs">
                        {req.name}
                      </code>
                      {req.description && (
                        <span className="text-muted-foreground">
                          {req.description}
                        </span>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {selectedDefinition && (
              <TriggerConfigFields
                definition={selectedDefinition}
                config={config}
                onChange={setConfig}
              />
            )}

            <div>
              <Type variant="body" className="mb-1 font-medium">
                Target Kind
              </Type>
              <Select
                value={targetKind}
                onValueChange={(value) =>
                  setTargetKind(isTriggerTargetKind(value) ? value : undefined)
                }
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="Select a target kind..." />
                </SelectTrigger>
                <SelectContent>
                  {triggerTargetKindOptions.map((option) => (
                    <SelectItem
                      key={option.value}
                      value={option.value}
                      description={option.description}
                    >
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div>
              <Type variant="body" className="mb-1 font-medium">
                Target Display Name
              </Type>
              <Input
                value={targetDisplay}
                onChange={setTargetDisplay}
                placeholder="e.g. My Assistant"
              />
            </div>

            <div>
              <Type variant="body" className="mb-1 font-medium">
                Target Reference
              </Type>
              <Input
                value={targetRef}
                onChange={setTargetRef}
                placeholder="e.g. assistant ID or slug"
              />
            </div>
          </Stack>
        </div>

        <Dialog.Footer>
          {isEditing && (
            <div className="mr-auto">
              {confirmDelete ? (
                <Stack direction="horizontal" gap={2}>
                  <Button
                    variant="destructive-primary"
                    onClick={handleDelete}
                    disabled={isPending}
                  >
                    {deleteMutation.isPending
                      ? "Deleting..."
                      : "Confirm Delete"}
                  </Button>
                  <Button
                    variant="tertiary"
                    onClick={() => setConfirmDelete(false)}
                  >
                    Cancel
                  </Button>
                </Stack>
              ) : (
                <Button
                  variant="tertiary"
                  onClick={() => setConfirmDelete(true)}
                >
                  <Button.LeftIcon>
                    <Icon name="trash-2" className="h-4 w-4" />
                  </Button.LeftIcon>
                  <Button.Text>Delete</Button.Text>
                </Button>
              )}
            </div>
          )}
          <Button variant="tertiary" onClick={() => handleOpenChange(false)}>
            Cancel
          </Button>
          <Button
            onClick={isEditing ? handleUpdate : handleCreate}
            disabled={!isValid || isPending}
          >
            {isPending
              ? isEditing
                ? "Saving..."
                : "Creating..."
              : isEditing
                ? "Save Changes"
                : "Create Trigger"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

export default function TriggersIndex() {
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingTrigger, setEditingTrigger] = useState<TriggerInstance | null>(
    null,
  );

  const { data: triggersData, isLoading: triggersLoading } = useTriggers(
    undefined,
    { retry: false, throwOnError: false },
  );
  const { data: definitionsData, isLoading: definitionsLoading } =
    useTriggerDefinitions(undefined, {
      retry: false,
      throwOnError: false,
    });

  const triggers = triggersData?.triggers ?? [];
  const definitions = definitionsData?.definitions ?? [];
  const isLoading = triggersLoading || definitionsLoading;

  const openCreate = () => {
    setEditingTrigger(null);
    setDialogOpen(true);
  };

  const openEdit = (trigger: TriggerInstance) => {
    setEditingTrigger(trigger);
    setDialogOpen(true);
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Triggers</Page.Section.Title>
          <Page.Section.Description>
            Connect external events to your assistants via webhooks or cron
            schedules.
          </Page.Section.Description>
          <Page.Section.CTA>
            {triggers.length > 0 && (
              <Button onClick={openCreate}>
                <Button.LeftIcon>
                  <Icon name="plus" className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Create Trigger</Button.Text>
              </Button>
            )}
          </Page.Section.CTA>
          <Page.Section.Body>
            {isLoading ? (
              <Stack align="center" justify="center" className="py-16">
                <Icon
                  name="loader-circle"
                  className="text-muted-foreground h-6 w-6 animate-spin"
                />
              </Stack>
            ) : triggers.length === 0 ? (
              <TriggersEmptyState onCreate={openCreate} />
            ) : (
              <TriggersTable
                triggers={triggers}
                definitions={definitions}
                onEdit={openEdit}
              />
            )}
          </Page.Section.Body>
        </Page.Section>

        <TriggerDialog
          open={dialogOpen}
          onOpenChange={(open) => {
            setDialogOpen(open);
            if (!open) setEditingTrigger(null);
          }}
          editingTrigger={editingTrigger}
        />
      </Page.Body>
    </Page>
  );
}
