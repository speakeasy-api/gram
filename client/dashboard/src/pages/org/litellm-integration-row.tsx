import { CodeBlock } from "@/components/code";
import { InputField } from "@/components/moon/input-field";
import { Alert, ErrorAlert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Label } from "@/components/ui/Label";
import { MoreActions } from "@/components/ui/MoreActions";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import { getServerURL } from "@/lib/utils";
import type { LiteLLMInstance } from "@gram/client/models/components/litellminstance.js";
import type { LitellmInstanceKeyResult } from "@gram/client/models/components/litellminstancekeyresult.js";
import { useCreateLiteLLMInstanceMutation } from "@gram/client/react-query/createLiteLLMInstance.js";
import {
  invalidateAllLiteLLMInstances,
  useLiteLLMInstances,
} from "@gram/client/react-query/liteLLMInstances.js";
import { useRevokeLiteLLMInstanceMutation } from "@gram/client/react-query/revokeLiteLLMInstance.js";
import { useRotateLiteLLMInstanceKeyMutation } from "@gram/client/react-query/rotateLiteLLMInstanceKey.js";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, Network, Plus, RefreshCw } from "lucide-react";
import { FormEvent, useMemo, useState } from "react";
import { toast } from "sonner";
import { ConfirmDialog } from "../remote-identity-providers/ConfirmDialog";
import {
  buildLiteLLMEnvironment,
  buildLiteLLMGuardrailConfig,
  liteLLMVerificationCommands,
} from "./litellm-config";

type FailurePosture = LiteLLMInstance["failurePosture"];

export function LiteLLMIntegrationRow(): JSX.Element {
  const organization = useOrganization();
  const projects = useMemo(
    () =>
      [...organization.projects].sort((a, b) => a.name.localeCompare(b.name)),
    [organization.projects],
  );
  const defaultProject =
    projects.find((project) => project.slug === "default") ?? projects[0];
  const [expanded, setExpanded] = useState(false);
  const [projectSlug, setProjectSlug] = useState(defaultProject?.slug ?? "");
  const selectedProjectSlug = projectSlug || defaultProject?.slug || "";
  const [createOpen, setCreateOpen] = useState(false);
  const [setupInstance, setSetupInstance] = useState<LiteLLMInstance | null>(
    null,
  );
  const [diagnosticsInstanceID, setDiagnosticsInstanceID] = useState<
    string | null
  >(null);
  const [rotateTarget, setRotateTarget] = useState<LiteLLMInstance | null>(
    null,
  );
  const [revokeTarget, setRevokeTarget] = useState<LiteLLMInstance | null>(
    null,
  );
  const queryClient = useQueryClient();
  const instancesQuery = useLiteLLMInstances(
    { gramProject: selectedProjectSlug },
    undefined,
    {
      enabled: selectedProjectSlug !== "",
      refetchInterval: expanded ? 10_000 : false,
      throwOnError: false,
    },
  );
  const instances = instancesQuery.data?.instances ?? [];
  const diagnosticsInstance =
    instances.find((instance) => instance.id === diagnosticsInstanceID) ?? null;
  const activeCount = instances.filter((instance) => instance.active).length;
  let instanceSummary = `${activeCount} active ${activeCount === 1 ? "instance" : "instances"}`;
  if (instancesQuery.isPending) instanceSummary = "Loading instances";
  if (instancesQuery.error) instanceSummary = "Unable to load instances";

  const rotateMutation = useRotateLiteLLMInstanceKeyMutation({
    gcTime: 0,
    onSuccess: () => {
      void invalidateAllLiteLLMInstances(queryClient);
    },
  });
  const revokeMutation = useRevokeLiteLLMInstanceMutation({
    onSuccess: async () => {
      await invalidateAllLiteLLMInstances(queryClient);
      setRevokeTarget(null);
      toast.success("LiteLLM integration revoked");
    },
    onError: () => toast.error("Failed to revoke LiteLLM integration"),
  });

  const columns: Column<LiteLLMInstance>[] = [
    {
      key: "name",
      header: "Instance",
      width: "1fr",
      render: (instance) => (
        <Stack gap={1}>
          <Stack direction="horizontal" gap={2} align="center">
            <Text className="font-medium">{instance.name}</Text>
            {!instance.active && <Badge variant="neutral">Revoked</Badge>}
          </Stack>
          <Text muted small>
            {instance.project.name}
          </Text>
        </Stack>
      ),
    },
    {
      key: "health",
      header: "Health",
      width: "130px",
      render: (instance) => <HealthBadge instance={instance} />,
    },
    {
      key: "keyPrefix",
      header: "Key prefix",
      width: "150px",
      render: (instance) => (
        <Text mono small>
          {instance.keyPrefix}
        </Text>
      ),
    },
    {
      key: "failurePosture",
      header: "Failure posture",
      width: "150px",
      render: (instance) => (
        <FailurePostureBadge posture={instance.failurePosture} />
      ),
    },
    {
      key: "createdAt",
      header: "Created",
      width: "160px",
      render: (instance) => <HumanizeDateTime date={instance.createdAt} />,
    },
    {
      key: "lastUsedAt",
      header: "Last used",
      width: "160px",
      render: (instance) =>
        instance.lastUsedAt ? (
          <HumanizeDateTime date={instance.lastUsedAt} />
        ) : (
          <Text muted small>
            Never
          </Text>
        ),
    },
    {
      key: "actions",
      header: "",
      width: "52px",
      render: (instance) =>
        instance.active ? (
          <MoreActions
            actions={[
              {
                label: "View diagnostics",
                onClick: () => setDiagnosticsInstanceID(instance.id),
              },
              {
                label: "View setup",
                onClick: () => setSetupInstance(instance),
              },
              {
                label: "Rotate key",
                onClick: () => setRotateTarget(instance),
              },
              {
                label: "Revoke",
                destructive: true,
                onClick: () => setRevokeTarget(instance),
              },
            ]}
          />
        ) : null,
    },
  ];

  const handleRotate = () => {
    if (!rotateTarget) return;
    rotateMutation.mutate({
      request: {
        gramProject: rotateTarget.project.slug,
        riskIDRequestBody: { id: rotateTarget.id },
      },
    });
  };

  const closeRotateDialog = () => {
    setRotateTarget(null);
    rotateMutation.reset();
  };

  const handleRevoke = () => {
    if (!revokeTarget) return;
    revokeMutation.mutate({
      request: {
        gramProject: revokeTarget.project.slug,
        id: revokeTarget.id,
      },
    });
  };

  return (
    <div className="flex flex-col">
      <div className="hover:bg-muted/50 p-4 transition-colors">
        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          gap={4}
        >
          <button
            type="button"
            aria-expanded={expanded}
            aria-controls="litellm-instances-panel"
            aria-label={`${expanded ? "Hide" : "Show"} LiteLLM integrations`}
            onClick={() => setExpanded((current) => !current)}
            className="flex min-w-0 flex-1 cursor-pointer items-center justify-between gap-4 text-left focus-visible:outline-2 focus-visible:outline-offset-4"
          >
            <Stack gap={1} className="min-w-0 flex-1">
              <Stack direction="horizontal" align="center" gap={2}>
                <Network className="text-foreground h-4 w-4 shrink-0" />
                <Text className="font-medium">LiteLLM</Text>
                <Badge variant="information">Push</Badge>
              </Stack>
              <Text muted small className="ml-6 truncate">
                Enforce Gram risk policies and send LiteLLM usage telemetry to
                Gram.
              </Text>
            </Stack>
            <Stack direction="horizontal" align="center" gap={3}>
              <Text muted small className="hidden whitespace-nowrap @3xl:block">
                {instanceSummary}
              </Text>
              <ChevronDown
                aria-hidden
                className={`text-muted-foreground h-4 w-4 transition-transform ${expanded ? "rotate-180" : ""}`}
              />
            </Stack>
          </button>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setCreateOpen(true)}
            disabled={projects.length === 0}
          >
            <Button.LeftIcon>
              <Plus className="size-3.5" />
            </Button.LeftIcon>
            <Button.Text>New instance</Button.Text>
          </Button>
        </Stack>
      </div>

      {expanded && (
        <div id="litellm-instances-panel">
          <Stack gap={4} className="px-4 pb-4 pl-10">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
              <Stack gap={1}>
                <Label htmlFor="litellm-project">Project</Label>
                <Select
                  value={selectedProjectSlug}
                  onValueChange={setProjectSlug}
                >
                  <SelectTrigger id="litellm-project" className="min-w-56">
                    <SelectValue placeholder="Choose a project" />
                  </SelectTrigger>
                  <SelectContent>
                    {projects.map((project) => (
                      <SelectItem key={project.id} value={project.slug}>
                        {project.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Stack>
              <Stack direction="horizontal" align="center" gap={2}>
                <Text muted small>
                  Each instance has its own project-bound ingestion key.
                </Text>
                <Button
                  variant="tertiary"
                  size="sm"
                  onClick={() => void instancesQuery.refetch()}
                  disabled={instancesQuery.isFetching}
                >
                  <Button.LeftIcon>
                    <RefreshCw
                      className={`size-3.5 ${instancesQuery.isFetching ? "animate-spin" : ""}`}
                    />
                  </Button.LeftIcon>
                  <Button.Text>Refresh</Button.Text>
                </Button>
              </Stack>
            </div>

            {instancesQuery.isPending ? <SkeletonTable /> : null}
            {instancesQuery.error ? (
              <ErrorAlert
                title="Unable to load LiteLLM integrations"
                error={instancesQuery.error}
              />
            ) : null}
            {!instancesQuery.isPending && !instancesQuery.error ? (
              <Table
                columns={columns}
                data={instances}
                rowKey={(instance) => instance.id}
                noResultsMessage={
                  <div className="px-4 py-6">
                    <Text muted>No LiteLLM instances in this project.</Text>
                  </div>
                }
              />
            ) : null}
          </Stack>
        </div>
      )}

      {createOpen ? (
        <CreateInstanceDialog
          open
          onOpenChange={setCreateOpen}
          projects={projects}
          initialProjectSlug={selectedProjectSlug}
          onProjectCreated={setProjectSlug}
        />
      ) : null}
      <SetupDialog
        instance={setupInstance}
        onOpenChange={(open) => {
          if (!open) setSetupInstance(null);
        }}
      />
      <DiagnosticsDialog
        instance={diagnosticsInstance}
        onOpenChange={(open) => {
          if (!open) setDiagnosticsInstanceID(null);
        }}
      />
      <RotateKeyDialog
        target={rotateTarget}
        result={rotateMutation.data}
        error={rotateMutation.error}
        isPending={rotateMutation.isPending}
        onConfirm={handleRotate}
        onClose={closeRotateDialog}
      />
      <RevokeInstanceDialog
        target={revokeTarget}
        isPending={revokeMutation.isPending}
        onConfirm={handleRevoke}
        onClose={() => setRevokeTarget(null)}
      />
    </div>
  );
}

export function CreateInstanceDialog({
  open,
  onOpenChange,
  projects,
  initialProjectSlug,
  onProjectCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projects: Array<{ id: string; name: string; slug: string }>;
  initialProjectSlug: string;
  onProjectCreated: (projectSlug: string) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [projectSlug, setProjectSlug] = useState(initialProjectSlug);
  const [name, setName] = useState("");
  const [failurePosture, setFailurePosture] =
    useState<FailurePosture>("fail_closed");
  const mutation = useCreateLiteLLMInstanceMutation({
    gcTime: 0,
    onSuccess: (data) => {
      void invalidateAllLiteLLMInstances(queryClient);
      onProjectCreated(data.instance.project.slug);
    },
  });

  const closeDialog = () => {
    onOpenChange(false);
    setName("");
    setFailurePosture("fail_closed");
    mutation.reset();
  };

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      onOpenChange(true);
      return;
    }
    if (mutation.isPending || mutation.data) return;
    closeDialog();
  };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    mutation.mutate({
      request: {
        gramProject: projectSlug,
        createInstanceRequestBody: {
          name: name.trim(),
          failurePosture,
        },
      },
    });
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content
        closeable={!mutation.isPending && !mutation.data}
        className={
          mutation.data ? "max-h-[90vh] max-w-3xl overflow-y-auto" : undefined
        }
      >
        {mutation.data ? (
          <>
            <Dialog.Header>
              <Dialog.Title>LiteLLM integration created</Dialog.Title>
              <Dialog.Description>
                Copy the key and generated configuration before closing this
                dialog.
              </Dialog.Description>
            </Dialog.Header>
            <SetupContent result={mutation.data} />
            <Dialog.Footer>
              <Button onClick={closeDialog}>I have saved the key</Button>
            </Dialog.Footer>
          </>
        ) : (
          <>
            <Dialog.Header>
              <Dialog.Title>Create LiteLLM integration</Dialog.Title>
              <Dialog.Description>
                Provision a dedicated ingestion key for one LiteLLM deployment
                and Gram project.
              </Dialog.Description>
            </Dialog.Header>
            <form className="space-y-5" onSubmit={handleSubmit}>
              <Stack gap={2}>
                <Label htmlFor="litellm-create-project">Project</Label>
                <Select value={projectSlug} onValueChange={setProjectSlug}>
                  <SelectTrigger id="litellm-create-project" className="w-full">
                    <SelectValue placeholder="Choose a project" />
                  </SelectTrigger>
                  <SelectContent>
                    {projects.map((project) => (
                      <SelectItem key={project.id} value={project.slug}>
                        {project.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Stack>
              <InputField
                label="Instance name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="Production LiteLLM"
                maxLength={255}
                required
                autoFocus
              />
              <Stack gap={2}>
                <Label id="litellm-failure-posture-label">
                  Failure posture
                </Label>
                <RadioGroup
                  aria-labelledby="litellm-failure-posture-label"
                  value={failurePosture}
                  onValueChange={(value) =>
                    setFailurePosture(value as FailurePosture)
                  }
                >
                  <label className="border-border flex cursor-pointer gap-3 border p-3">
                    <RadioGroupItem value="fail_closed" />
                    <Stack gap={1}>
                      <Text className="font-medium">
                        Fail closed (recommended)
                      </Text>
                      <Text muted small>
                        Block model requests when Gram cannot evaluate them.
                      </Text>
                    </Stack>
                  </label>
                  <label className="border-warning-softest flex cursor-pointer gap-3 border p-3">
                    <RadioGroupItem value="fail_open" />
                    <Stack gap={1}>
                      <Text className="font-medium">Fail open</Text>
                      <Text muted small>
                        Allow model requests during a Gram outage. This is an
                        explicit security posture decision.
                      </Text>
                    </Stack>
                  </label>
                </RadioGroup>
              </Stack>
              {mutation.error ? <ErrorAlert error={mutation.error} /> : null}
              <Dialog.Footer>
                <Button
                  type="button"
                  variant="tertiary"
                  onClick={() => handleOpenChange(false)}
                  disabled={mutation.isPending}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={
                    mutation.isPending ||
                    projectSlug === "" ||
                    name.trim() === ""
                  }
                >
                  {mutation.isPending ? "Creating…" : "Create integration"}
                </Button>
              </Dialog.Footer>
            </form>
          </>
        )}
      </Dialog.Content>
    </Dialog>
  );
}

function SetupDialog({
  instance,
  onOpenChange,
}: {
  instance: LiteLLMInstance | null;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  return (
    <Dialog open={instance !== null} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-h-[90vh] max-w-3xl overflow-y-auto">
        <Dialog.Header>
          <Dialog.Title>Configure {instance?.name}</Dialog.Title>
          <Dialog.Description>
            Add the generated YAML and environment variables to your LiteLLM
            proxy.
          </Dialog.Description>
        </Dialog.Header>
        {instance ? <SetupContent instance={instance} /> : null}
        <Dialog.Footer>
          <Button onClick={() => onOpenChange(false)}>Close</Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function DiagnosticsDialog({
  instance,
  onOpenChange,
}: {
  instance: LiteLLMInstance | null;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  return (
    <Dialog open={instance !== null} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-3xl">
        <Dialog.Header>
          <Dialog.Title>{instance?.name} diagnostics</Dialog.Title>
          <Dialog.Description>
            Connection health and identity attribution for the last 24 hours.
          </Dialog.Description>
        </Dialog.Header>
        {instance ? <DiagnosticsPanel instance={instance} /> : null}
        <Dialog.Footer>
          <Button onClick={() => onOpenChange(false)}>Close</Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

export function RotateKeyDialog({
  target,
  result,
  error,
  isPending,
  onConfirm,
  onClose,
}: {
  target: LiteLLMInstance | null;
  result: LitellmInstanceKeyResult | undefined;
  error: Error | null;
  isPending: boolean;
  onConfirm: () => void;
  onClose: () => void;
}): JSX.Element {
  return (
    <Dialog
      open={target !== null}
      onOpenChange={(open) => {
        if (!open && !isPending && !result) onClose();
      }}
    >
      <Dialog.Content
        closeable={!isPending && !result}
        className={
          result ? "max-h-[90vh] max-w-3xl overflow-y-auto" : undefined
        }
      >
        {result ? (
          <>
            <Dialog.Header>
              <Dialog.Title>LiteLLM key rotated</Dialog.Title>
              <Dialog.Description>
                Replace the old key immediately. It is no longer valid.
              </Dialog.Description>
            </Dialog.Header>
            <SetupContent result={result} />
            <Dialog.Footer>
              <Button onClick={onClose}>I have saved the key</Button>
            </Dialog.Footer>
          </>
        ) : (
          <>
            <Dialog.Header>
              <Dialog.Title>Rotate LiteLLM key</Dialog.Title>
              <Dialog.Description>
                Rotate the key for <strong>{target?.name}</strong>? The current
                key stops working immediately.
              </Dialog.Description>
            </Dialog.Header>
            {error ? <ErrorAlert error={error} /> : null}
            <Dialog.Footer>
              <Button variant="tertiary" onClick={onClose} disabled={isPending}>
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={onConfirm}
                disabled={isPending}
              >
                {isPending ? "Rotating…" : "Rotate key"}
              </Button>
            </Dialog.Footer>
          </>
        )}
      </Dialog.Content>
    </Dialog>
  );
}

export function RevokeInstanceDialog({
  target,
  isPending,
  onConfirm,
  onClose,
}: {
  target: LiteLLMInstance | null;
  isPending: boolean;
  onConfirm: () => void;
  onClose: () => void;
}): JSX.Element {
  return (
    <ConfirmDialog
      open={target !== null}
      onOpenChange={(open) => {
        if (!open && !isPending) onClose();
      }}
      title="Revoke LiteLLM integration"
      description={
        <>
          Revoke <strong>{target?.name}</strong> and immediately invalidate its
          ingestion key? This cannot be undone.
        </>
      }
      confirmLabel="Revoke integration"
      onConfirm={onConfirm}
      isPending={isPending}
    />
  );
}

function SetupContent({
  instance: instanceProp,
  result,
}: {
  instance?: LiteLLMInstance;
  result?: LitellmInstanceKeyResult;
}): JSX.Element {
  const instance = result?.instance ?? instanceProp;
  if (!instance) return <></>;
  const serverURL = getServerURL();

  return (
    <Stack gap={5}>
      {result ? (
        <Alert variant="warning">
          This key is shown once. Copy it now and store it securely.
        </Alert>
      ) : (
        <Alert variant="info">
          Gram no longer has the plaintext key. Replace the placeholder with the
          key stored by your team.
        </Alert>
      )}
      {result ? (
        <SetupSection
          title="Integration key"
          description="Store this dedicated project-bound key securely. Gram cannot show it again."
        >
          <CodeBlock copyLabel="integration key">{result.key}</CodeBlock>
        </SetupSection>
      ) : null}
      <SetupSection
        title="Environment variables"
        description="Paste the key into the first variable, then set these on the LiteLLM proxy."
      >
        <CodeBlock language="shell" copyLabel="environment variables">
          {buildLiteLLMEnvironment(serverURL, instance.project.slug)}
        </CodeBlock>
      </SetupSection>
      <SetupSection
        title="LiteLLM configuration"
        description="Merge this guardrail fragment into your LiteLLM configuration."
      >
        <CodeBlock language="yaml" copyLabel="LiteLLM configuration">
          {buildLiteLLMGuardrailConfig(serverURL, instance.failurePosture)}
        </CodeBlock>
      </SetupSection>
      <SetupSection
        title="Verify safe traffic"
        description="Set LITELLM_VIRTUAL_KEY and LITELLM_MODEL in your shell, then run this against the proxy."
      >
        <CodeBlock language="shell" copyLabel="safe traffic command">
          {liteLLMVerificationCommands.safe}
        </CodeBlock>
      </SetupSection>
      <SetupSection
        title="Verify blocking"
        description="This synthetic credential should be blocked when the project secret policy is enabled."
      >
        <CodeBlock language="shell" copyLabel="blocking test command">
          {liteLLMVerificationCommands.blocked}
        </CodeBlock>
      </SetupSection>
    </Stack>
  );
}

function SetupSection({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <Stack gap={2}>
      <Text as="h3" className="font-medium">
        {title}
      </Text>
      <Text muted small>
        {description}
      </Text>
      {children}
    </Stack>
  );
}

function HealthBadge({ instance }: { instance: LiteLLMInstance }): JSX.Element {
  if (!instance.active) return <Badge variant="neutral">Revoked</Badge>;
  switch (instance.diagnostics.status) {
    case "success":
      return <Badge variant="success">Connected</Badge>;
    case "failed":
      return <Badge variant="destructive">Error</Badge>;
    case "pending":
      return <Badge variant="neutral">Waiting for traffic</Badge>;
  }
}

function FailurePostureBadge({
  posture,
}: {
  posture: FailurePosture;
}): JSX.Element {
  return posture === "fail_closed" ? (
    <Badge variant="neutral">Fail closed</Badge>
  ) : (
    <Badge variant="warning">Fail open</Badge>
  );
}

export function DiagnosticsPanel({
  instance,
}: {
  instance: LiteLLMInstance;
}): JSX.Element {
  const diagnostics = instance.diagnostics;
  return (
    <div className="bg-muted/20 grid grid-cols-1 gap-px border-t sm:grid-cols-2 lg:grid-cols-4">
      <Diagnostic
        label="Connection health"
        content={<HealthBadge instance={instance} />}
      />
      <Diagnostic
        label="LiteLLM version"
        value={diagnostics.reportedLitellmVersion ?? "Not reported"}
      />
      <Diagnostic
        label="Last ingestion"
        date={diagnostics.lastGuardrailEventAt}
      />
      <Diagnostic label="Last OTel event" date={diagnostics.lastOtelEventAt} />
      <Diagnostic
        label="Last error"
        date={diagnostics.lastErrorAt}
        value={errorKindLabel(diagnostics.lastErrorKind)}
      />
      <Diagnostic
        label="Virtual-key email (24h)"
        value={formatPercentage(diagnostics.virtualKeyEmailPct24h)}
      />
      <Diagnostic
        label="Gram user attribution (24h)"
        value={formatPercentage(diagnostics.platformUserPct24h)}
      />
      <Diagnostic label="Created" date={instance.createdAt} />
      <Diagnostic label="Project" value={instance.project.name} />
    </div>
  );
}

function Diagnostic({
  label,
  value,
  date,
  content,
}: {
  label: string;
  value?: string;
  date?: Date;
  content?: React.ReactNode;
}): JSX.Element {
  return (
    <Stack gap={1} className="bg-background p-3">
      <Text muted small>
        {label}
      </Text>
      {content}
      {value ? <Text small>{value}</Text> : null}
      {date ? (
        <Text small>
          <HumanizeDateTime date={date} />
        </Text>
      ) : !value && !content ? (
        <Text muted small>
          Not received
        </Text>
      ) : null}
    </Stack>
  );
}

function formatPercentage(value: number | undefined): string {
  return value === undefined ? "No recent traffic" : `${value.toFixed(1)}%`;
}

function errorKindLabel(
  kind: LiteLLMInstance["diagnostics"]["lastErrorKind"],
): string | undefined {
  switch (kind) {
    case "auth_failure":
      return "Authentication failure";
    case "decode_failure":
      return "Invalid payload";
    case "limit_exceeded":
      return "Payload limit exceeded";
    case undefined:
      return undefined;
  }
}
