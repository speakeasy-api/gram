import { RequireScope } from "@/components/require-scope";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useCustomDomains } from "@/hooks/useToolsetUrl";
import { useRBAC } from "@/hooks/useRBAC";
import { getServerURL } from "@/lib/utils";
import { mcpServerRouteParam } from "@/lib/sources";
import { toolVariationsGroupDisplayName } from "@/lib/toolVariationGroups";
import { cn } from "@/lib/utils";
import { useOrgRoutes, useRoutes } from "@/routes";
import type {
  CustomDomain,
  McpEndpoint,
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components";
import {
  buildGetMcpServerQuery,
  invalidateAllGetMcpServer,
  invalidateAllMcpEndpoints,
  invalidateAllMcpServers,
  invalidateAllToolVariationGroups,
  useCreateGlobalToolVariationGroupMutation,
  useDeleteMcpEndpointMutation,
  useDeleteMcpServerMutation,
  useToolVariationGroups,
  useUpdateMcpEndpointMutation,
  useUpdateMcpServerMutation,
} from "@gram/client/react-query/index.js";
import { Alert, Button, Dialog, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus, SaveIcon, Trash2, XIcon } from "lucide-react";
import { createContext, use, useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { toast } from "sonner";
import { useMcpEndpointSlugValidation } from "../useMcpEndpointSlugValidation";

// The display name shares the mcp_servers.name column, whose CHECK caps length
// at 40 (see schema.sql / MCP_SERVER_NAME_MAX_LENGTH on the legacy page).
const NAME_MAX_LENGTH = 40;
const ADDRESS_INPUT_GROUP_CLASSNAME = "rounded-md";
const ADDRESS_SLUG_INPUT_CLASSNAME = "font-mono pl-0! font-bold";
const ADDRESS_RANDOM_SUFFIX_ALPHABET = "abcdefghijklmnopqrstuvwxyz0123456789";
const ADDRESS_RANDOM_SUFFIX_LENGTH = 5;
export const MCP_SERVER_URL_SECTION_ID = "server-url";

function generateAddressSuffix() {
  let suffix = "";
  for (let i = 0; i < ADDRESS_RANDOM_SUFFIX_LENGTH; i += 1) {
    const index = Math.floor(
      Math.random() * ADDRESS_RANDOM_SUFFIX_ALPHABET.length,
    );
    suffix += ADDRESS_RANDOM_SUFFIX_ALPHABET[index];
  }
  return suffix;
}

function useScrollToSettingsHash() {
  const location = useLocation();

  useEffect(() => {
    const targetId = location.hash.replace("#", "");
    if (targetId !== MCP_SERVER_URL_SECTION_ID) return;

    const animationFrame = window.requestAnimationFrame(() => {
      document
        .getElementById(targetId)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    });

    return () => window.cancelAnimationFrame(animationFrame);
  }, [location.hash]);
}

export function SettingsTab({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
}): JSX.Element {
  useScrollToSettingsHash();

  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-10 px-8 py-8">
      <DisplayNameCard mcpServer={mcpServer} />
      <ServerUrlCard
        mcpServer={mcpServer}
        endpoints={endpoints}
        isLoadingEndpoints={isLoadingEndpoints}
      />
      <ToolFilteringCard mcpServer={mcpServer} />
      <DangerZoneCard mcpServer={mcpServer} endpoints={endpoints} />
    </div>
  );
}

// --- Section shell --------------------------------------------------------

type SettingsSectionTone = "default" | "danger";
type SettingsSectionContextValue = {
  tone: SettingsSectionTone;
};
type SettingsSectionSlotProps = {
  children?: React.ReactNode;
  className?: string;
};

const DEFAULT_SETTINGS_SECTION_CONTEXT: SettingsSectionContextValue = {
  tone: "default",
};
const DANGER_SETTINGS_SECTION_CONTEXT: SettingsSectionContextValue = {
  tone: "danger",
};
const SettingsSectionContext = createContext(DEFAULT_SETTINGS_SECTION_CONTEXT);

function SettingsSectionRoot({
  children,
  id,
}: {
  children: React.ReactNode;
  id?: string;
}) {
  return (
    <SettingsSectionContext.Provider value={DEFAULT_SETTINGS_SECTION_CONTEXT}>
      <section id={id} className="space-y-3 scroll-mt-4">
        {children}
      </section>
    </SettingsSectionContext.Provider>
  );
}

function DangerSettingsSectionRoot({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <SettingsSectionContext.Provider value={DANGER_SETTINGS_SECTION_CONTEXT}>
      <section className="space-y-3">{children}</section>
    </SettingsSectionContext.Provider>
  );
}

function SettingsSectionHeader({
  children,
  className,
}: SettingsSectionSlotProps) {
  return <div className={cn("space-y-1", className)}>{children}</div>;
}

function SettingsSectionTitle({
  children,
  className,
}: SettingsSectionSlotProps) {
  const { tone } = use(SettingsSectionContext);

  return (
    <Heading
      variant="h4"
      className={cn(
        "normal-case",
        tone === "danger" && "text-destructive",
        className,
      )}
    >
      {children}
    </Heading>
  );
}

function SettingsSectionDescription({
  children,
  className,
}: SettingsSectionSlotProps) {
  return (
    <Type muted small className={cn("max-w-3xl", className)}>
      {children}
    </Type>
  );
}

function SettingsSectionPanel({
  children,
  className,
}: SettingsSectionSlotProps) {
  const { tone } = use(SettingsSectionContext);

  return (
    <div
      className={cn(
        "overflow-hidden rounded-xl border bg-card",
        tone === "danger" && "border-destructive/30",
        className,
      )}
    >
      {children}
    </div>
  );
}

function SettingsSectionBody({
  children,
  className,
}: SettingsSectionSlotProps) {
  return <div className={cn("space-y-4 p-6", className)}>{children}</div>;
}

function SettingsSectionFooter({
  children,
  className,
}: SettingsSectionSlotProps) {
  const { tone } = use(SettingsSectionContext);

  return (
    <div
      className={cn(
        "flex min-h-[56px] items-center justify-between gap-4 border-t px-6 py-3",
        tone === "danger" ? "bg-destructive/5" : "bg-muted/30",
        className,
      )}
    >
      {children}
    </div>
  );
}

function SettingsSectionFooterHint({
  children,
  className,
}: SettingsSectionSlotProps) {
  return (
    <Type muted small className={className}>
      {children}
    </Type>
  );
}

function SettingsSectionFooterActions({
  children,
  className,
}: SettingsSectionSlotProps) {
  return (
    <div className={cn("flex shrink-0 items-center gap-2", className)}>
      {children}
    </div>
  );
}

const settingsSectionSlots = {
  Header: SettingsSectionHeader,
  Title: SettingsSectionTitle,
  Description: SettingsSectionDescription,
  Panel: SettingsSectionPanel,
  Body: SettingsSectionBody,
  Footer: SettingsSectionFooter,
  FooterHint: SettingsSectionFooterHint,
  FooterActions: SettingsSectionFooterActions,
};

const SettingsSection = Object.assign(
  SettingsSectionRoot,
  settingsSectionSlots,
);
const DangerSettingsSection = Object.assign(
  DangerSettingsSectionRoot,
  settingsSectionSlots,
);

function FooterSaveButtonContent({ pending }: { pending: boolean }) {
  if (pending) {
    return (
      <>
        <Button.LeftIcon>
          <Loader2 className="size-4 animate-spin" />
        </Button.LeftIcon>
        <Button.Text>Saving</Button.Text>
      </>
    );
  }

  return (
    <>
      <Button.LeftIcon>
        <SaveIcon className="size-4" />
      </Button.LeftIcon>
      <Button.Text>Save</Button.Text>
    </>
  );
}

function RowSaveButtonContent({ pending }: { pending: boolean }) {
  if (pending) {
    return (
      <Button.LeftIcon>
        <Loader2 className="size-4 animate-spin" />
      </Button.LeftIcon>
    );
  }

  return (
    <Button.LeftIcon>
      <SaveIcon className="size-4" />
    </Button.LeftIcon>
  );
}

// --- Display name ---------------------------------------------------------

function DisplayNameCard({ mcpServer }: { mcpServer: McpServer }) {
  const [nameDraft, setNameDraft] = useState(mcpServer.name ?? "");

  // Re-sync draft when the upstream record changes (e.g. another tab edited
  // it or a refetch landed). Without this a stale draft survives the refetch.
  useEffect(() => {
    setNameDraft(mcpServer.name ?? "");
  }, [mcpServer.id, mcpServer.name]);

  const queryClient = useQueryClient();
  const update = useUpdateMcpServerMutation();
  const navigate = useNavigate();
  const routes = useRoutes();

  const trimmedDraft = nameDraft.trim();
  const dirty = trimmedDraft !== (mcpServer.name ?? "").trim();
  const saveDisabled =
    !dirty ||
    trimmedDraft === "" ||
    trimmedDraft.length > NAME_MAX_LENGTH ||
    update.isPending;
  const characterCount = `${nameDraft.length} of ${NAME_MAX_LENGTH} characters used`;

  const handleSave = async () => {
    try {
      const updated = await update.mutateAsync({
        request: {
          updateMcpServerForm: {
            id: mcpServer.id,
            name: trimmedDraft,
            remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
            toolsetId: mcpServer.toolsetId ?? undefined,
            environmentId: mcpServer.environmentId ?? undefined,
            userSessionIssuerId: mcpServer.userSessionIssuerId ?? undefined,
            toolVariationsGroupId: mcpServer.toolVariationsGroupId ?? undefined,
            visibility: mcpServer.visibility,
          },
        },
      });
      // The server recomputes slug on every update, so a name change produces
      // a new slug. Replace the route param with the new slug *before*
      // invalidating queries so the refetch uses the new lookup args and the
      // page-level not-found guard doesn't bounce the user back to /mcp.
      const nextParam = mcpServerRouteParam(updated);
      void navigate(routes.mcp.x.settings.href(nextParam), { replace: true });
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("MCP server updated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update MCP server";
      toast.error(message);
    }
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Branding</SettingsSection.Title>
        <SettingsSection.Description>
          Used to identify your MCP server within the dashboard and on its
          installation page.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field
            data-invalid={update.isError ? true : undefined}
            className="max-w-md"
          >
            <FieldLabel htmlFor="mcp-server-display-name">
              Display Name
            </FieldLabel>
            <Input
              id="mcp-server-display-name"
              value={nameDraft}
              onChange={(value) => setNameDraft(value)}
              placeholder="My MCP server"
              maxLength={NAME_MAX_LENGTH}
              aria-invalid={update.isError}
            />
            {dirty && (
              <FieldDescription className="pl-1 text-xs">
                {characterCount}
              </FieldDescription>
            )}
            {update.isError && <FieldError>{update.error.message}</FieldError>}
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {`Please use no more than ${NAME_MAX_LENGTH} characters.`}
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope scope="mcp:write" level="component">
              <Button
                variant="primary"
                size="md"
                disabled={saveDisabled}
                onClick={() => void handleSave()}
              >
                <FooterSaveButtonContent pending={update.isPending} />
              </Button>
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

// --- Server URL -----------------------------------------------------------

function ServerUrlCard({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
}) {
  const { domains } = useCustomDomains();
  const orgRoutes = useOrgRoutes();
  const { hasScope } = useRBAC();
  const canManageDomains = hasScope("org:admin");

  const platformEndpoint = useMemo(
    () => endpoints.find((e) => !e.customDomainId),
    [endpoints],
  );
  const customDomainEndpoints = useMemo(
    () => endpoints.filter((e) => !!e.customDomainId),
    [endpoints],
  );
  const availableDomains = useMemo(
    () => domains.filter((d): d is CustomDomain => d != null),
    [domains],
  );

  const [addingPlatform, setAddingPlatform] = useState(false);
  const [addingCustom, setAddingCustom] = useState(false);

  let customAddressEmptyState: React.ReactNode = null;
  if (!addingCustom && customDomainEndpoints.length === 0) {
    if (availableDomains.length === 0) {
      let description =
        "Ask an organization administrator to add and verify a custom domain.";
      let actionLabel: string | undefined;
      let onAction: (() => void) | undefined;

      if (canManageDomains) {
        description =
          "Add a custom domain before creating a custom MCP address.";
        actionLabel = "Add custom domain";
        onAction = () => orgRoutes.domains.goTo();
      }

      customAddressEmptyState = (
        <AddressEmptyState
          title="No custom domains"
          description={description}
          actionLabel={actionLabel}
          onAction={onAction}
        />
      );
    } else {
      customAddressEmptyState = (
        <RequireScope scope="mcp:write" level="component">
          <AddressEmptyState
            title="No custom address"
            description="Create an MCP URL on your verified custom domain."
            actionLabel="Add"
            onAction={() => setAddingCustom(true)}
          />
        </RequireScope>
      );
    }
  }

  return (
    <SettingsSection id={MCP_SERVER_URL_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Server URL</SettingsSection.Title>
        <SettingsSection.Description>
          The web address MCP clients use to connect to this server.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {isLoadingEndpoints ? (
            <Type muted small>
              Loading…
            </Type>
          ) : (
            <FieldGroup className="gap-6">
              {/* Hosted (platform) address: at most one. */}
              <Field>
                <FieldLabel>Hosted Address</FieldLabel>
                {platformEndpoint ? (
                  <AddressRow
                    mcpServer={mcpServer}
                    endpoint={platformEndpoint}
                  />
                ) : addingPlatform ? (
                  <NewPlatformAddressRow
                    mcpServer={mcpServer}
                    onClose={() => setAddingPlatform(false)}
                  />
                ) : (
                  <RequireScope scope="mcp:write" level="component">
                    <AddressEmptyState
                      title="No hosted address"
                      description="Create the default Speakeasy-hosted URL for this server."
                      actionLabel="Add"
                      onAction={() => setAddingPlatform(true)}
                    />
                  </RequireScope>
                )}
                <FieldDescription>
                  Hosted under a Speakeasy domain. Always available unless you
                  remove it.
                </FieldDescription>
              </Field>

              {/* Custom-domain addresses: zero or more. */}
              <Field>
                <div className="flex items-center gap-2">
                  <FieldLabel>Custom Address</FieldLabel>
                </div>
                {customDomainEndpoints.map((endpoint) => (
                  <AddressRow
                    key={endpoint.id}
                    mcpServer={mcpServer}
                    endpoint={endpoint}
                    domains={availableDomains}
                  />
                ))}
                {addingCustom && (
                  <NewCustomAddressRow
                    mcpServer={mcpServer}
                    domains={availableDomains}
                    onClose={() => setAddingCustom(false)}
                  />
                )}
                {customAddressEmptyState}
                {!addingCustom &&
                  customDomainEndpoints.length > 0 &&
                  availableDomains.length > 0 && (
                    <RequireScope scope="mcp:write" level="component">
                      <div>
                        <Button
                          variant="secondary"
                          onClick={() => setAddingCustom(true)}
                        >
                          <Button.LeftIcon>
                            <Plus className="size-4" />
                          </Button.LeftIcon>
                          <Button.Text>Add</Button.Text>
                        </Button>
                      </div>
                    </RequireScope>
                  )}
              </Field>
            </FieldGroup>
          )}
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Changes apply to new client connections.
          </SettingsSection.FooterHint>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
function AddressEmptyState({
  title,
  description,
  actionLabel,
  onAction,
}: {
  title: string;
  description: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <div className="bg-muted/20 flex min-h-[88px] flex-col items-start justify-between gap-3 rounded-md border border-dashed px-4 py-3 sm:flex-row sm:items-center">
      <div className="min-w-0 space-y-1">
        <Type small className="font-medium">
          {title}
        </Type>
        <Type muted small className="max-w-xl">
          {description}
        </Type>
      </div>
      {actionLabel && onAction && (
        <Button
          size="md"
          variant="secondary"
          className="shrink-0"
          onClick={onAction}
        >
          <Button.LeftIcon>
            <Plus className="size-4" />
          </Button.LeftIcon>
          <Button.Text>{actionLabel}</Button.Text>
        </Button>
      )}
    </div>
  );
}

// A single editable address. The slug input is always live; Save persists the
// edit (disabled until dirty + valid) and Remove deletes immediately.
function AddressRow({
  mcpServer,
  endpoint,
  domains,
}: {
  mcpServer: McpServer;
  endpoint: McpEndpoint;
  domains?: CustomDomain[];
}) {
  const { orgSlug } = useSlugs();
  // Platform endpoints must carry the `${orgSlug}-` prefix. It's folded into
  // the read-only URL segment so the editable field holds just the suffix;
  // custom-domain endpoints have no such prefix.
  const slugPrefix = !endpoint.customDomainId && orgSlug ? `${orgSlug}-` : "";

  const [suffix, setSuffix] = useState(() =>
    endpoint.slug.startsWith(slugPrefix)
      ? endpoint.slug.slice(slugPrefix.length)
      : endpoint.slug,
  );
  useEffect(() => {
    setSuffix(
      endpoint.slug.startsWith(slugPrefix)
        ? endpoint.slug.slice(slugPrefix.length)
        : endpoint.slug,
    );
  }, [endpoint.slug, slugPrefix]);

  const fullSlug = `${slugPrefix}${suffix.trim()}`;

  const queryClient = useQueryClient();
  const update = useUpdateMcpEndpointMutation({
    onSuccess: async () => {
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Address updated");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to update address",
      );
    },
  });
  const remove = useDeleteMcpEndpointMutation({
    onSuccess: async () => {
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Address removed");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to remove address",
      );
    },
  });
  const slugError = useMcpEndpointSlugValidation(
    fullSlug,
    endpoint.customDomainId ?? null,
    endpoint.slug,
  );

  const dirty = fullSlug !== endpoint.slug;

  const customDomainLabel =
    endpoint.customDomainId &&
    domains?.find((d) => d.id === endpoint.customDomainId)?.domain;
  const baseUrlPrefix = customDomainLabel
    ? `https://${customDomainLabel}/mcp/`
    : `${getServerURL()}/mcp/`;
  const handleSave = () => {
    update.mutate({
      request: {
        updateMcpEndpointForm: {
          id: endpoint.id,
          mcpServerId: mcpServer.id,
          slug: fullSlug,
          customDomainId: endpoint.customDomainId ?? undefined,
        },
      },
    });
  };

  return (
    <Field
      data-invalid={!!slugError || update.isError ? true : undefined}
      className="gap-2"
    >
      <Stack direction="horizontal" gap={2} align="center">
        <InputGroup className={ADDRESS_INPUT_GROUP_CLASSNAME}>
          <InputGroupAddon>{`${baseUrlPrefix}${slugPrefix}`}</InputGroupAddon>
          <InputGroupInput
            value={suffix}
            onChange={(e) => setSuffix(e.target.value)}
            placeholder="endpoint"
            aria-invalid={!!slugError}
            className={ADDRESS_SLUG_INPUT_CLASSNAME}
          />
        </InputGroup>
        <RequireScope scope="mcp:write" level="component">
          <Button
            size="md"
            variant="primary"
            disabled={!dirty || !!slugError || update.isPending}
            onClick={handleSave}
          >
            <RowSaveButtonContent pending={update.isPending} />
          </Button>
          <Button
            variant="destructive-secondary"
            className="border border-destructive/40 bg-transparent hover:border-destructive"
            disabled={remove.isPending}
            onClick={() => remove.mutate({ request: { id: endpoint.id } })}
          >
            <Button.LeftIcon>
              <Trash2 className="size-4" />
            </Button.LeftIcon>
          </Button>
        </RequireScope>
      </Stack>
      {slugError && <FieldError className="text-xs">{slugError}</FieldError>}
      {update.isError && <FieldError>{update.error.message}</FieldError>}
    </Field>
  );
}

function NewPlatformAddressRow({
  mcpServer,
  onClose,
}: {
  mcpServer: McpServer;
  onClose: () => void;
}) {
  const [suffix, setSuffix] = useState(generateAddressSuffix);
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const { orgSlug } = useSlugs();
  const slugPrefix = orgSlug ? `${orgSlug}-` : "";
  const fullSlug = `${slugPrefix}${suffix.trim()}`;
  const slugError = useMcpEndpointSlugValidation(fullSlug, null);

  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const handleCreate = async () => {
    if (!suffix.trim() || slugError) return;
    setSubmitting(true);
    setErrorMsg(null);
    try {
      await client.mcpEndpoints.create({
        createMcpEndpointForm: {
          mcpServerId: mcpServer.id,
          slug: fullSlug,
        },
      });
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Address added");
      onClose();
    } catch (error) {
      setErrorMsg(
        error instanceof Error ? error.message : "Failed to add address",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Field
      data-invalid={
        (!!suffix.trim() && !!slugError) || errorMsg ? true : undefined
      }
      className="gap-2"
    >
      <Stack direction="horizontal" gap={2} align="center">
        <InputGroup className={ADDRESS_INPUT_GROUP_CLASSNAME}>
          <InputGroupAddon>
            {`${getServerURL()}/mcp/${slugPrefix}`}
          </InputGroupAddon>
          <InputGroupInput
            value={suffix}
            onChange={(e) => setSuffix(e.target.value)}
            placeholder="my-endpoint"
            aria-invalid={!!suffix.trim() && !!slugError}
            className={ADDRESS_SLUG_INPUT_CLASSNAME}
          />
        </InputGroup>
        <Button
          size="md"
          variant="primary"
          disabled={!suffix.trim() || !!slugError || submitting}
          onClick={() => void handleCreate()}
        >
          <RowSaveButtonContent pending={submitting} />
        </Button>
        <Button
          size="md"
          variant="secondary"
          disabled={submitting}
          onClick={onClose}
        >
          <Button.LeftIcon>
            <XIcon className="size-4" />
          </Button.LeftIcon>
        </Button>
      </Stack>
      {suffix.trim() && slugError && (
        <FieldError className="text-xs">{slugError}</FieldError>
      )}
      {errorMsg && <FieldError>{errorMsg}</FieldError>}
    </Field>
  );
}

function NewCustomAddressRow({
  mcpServer,
  domains,
  onClose,
}: {
  mcpServer: McpServer;
  domains: CustomDomain[];
  onClose: () => void;
}) {
  const customDomain = domains[0];
  const domainId = customDomain?.id ?? "";
  const [slug, setSlug] = useState("");
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const slugError = useMcpEndpointSlugValidation(slug.trim(), domainId || null);

  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const handleCreate = async () => {
    const trimmed = slug.trim();
    if (!trimmed || !domainId || slugError) return;
    setSubmitting(true);
    setErrorMsg(null);
    try {
      await client.mcpEndpoints.create({
        createMcpEndpointForm: {
          mcpServerId: mcpServer.id,
          slug: trimmed,
          customDomainId: domainId,
        },
      });
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Address added");
      onClose();
    } catch (error) {
      setErrorMsg(
        error instanceof Error ? error.message : "Failed to add address",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Field
      data-invalid={
        (!!slug.trim() && !!slugError) || errorMsg ? true : undefined
      }
      className="gap-2"
    >
      <Stack direction="horizontal" gap={2} align="center">
        <InputGroup className={ADDRESS_INPUT_GROUP_CLASSNAME}>
          <InputGroupAddon>{`https://${customDomain?.domain ?? "custom-domain"}/mcp/`}</InputGroupAddon>
          <InputGroupInput
            value={slug}
            onChange={(e) => setSlug(e.target.value)}
            placeholder="my-endpoint"
            aria-invalid={!!slug.trim() && !!slugError}
            className={ADDRESS_SLUG_INPUT_CLASSNAME}
          />
        </InputGroup>
        <Button
          size="md"
          variant="primary"
          disabled={!slug.trim() || !domainId || !!slugError || submitting}
          onClick={() => void handleCreate()}
        >
          <RowSaveButtonContent pending={submitting} />
        </Button>
        <Button
          size="md"
          variant="secondary"
          disabled={submitting}
          onClick={onClose}
        >
          <Button.LeftIcon>
            <XIcon className="size-4" />
          </Button.LeftIcon>
        </Button>
      </Stack>
      {slug.trim() && slugError && (
        <FieldError className="text-xs">{slugError}</FieldError>
      )}
      {errorMsg && <FieldError>{errorMsg}</FieldError>}
    </Field>
  );
}

// --- Tool filtering -------------------------------------------------------

// Radix Select disallows an empty-string value, so the "Disabled" option needs
// a sentinel that maps back to null (filtering off) when persisted.
const DISABLED_VALUE = "__disabled__";

function ToolFilteringCard({ mcpServer }: { mcpServer: McpServer }) {
  const queryClient = useQueryClient();
  const groupsQuery = useToolVariationGroups();
  const groups = groupsQuery.data?.groups ?? [];

  const currentValue = mcpServer.toolVariationsGroupId ?? DISABLED_VALUE;
  const [draft, setDraft] = useState(currentValue);

  // Re-sync the draft when the persisted value changes underneath us.
  useEffect(() => {
    setDraft(currentValue);
  }, [currentValue]);

  const notifyError = (error: unknown) =>
    toast.error(
      error instanceof Error
        ? error.message
        : "Failed to update tool filtering settings",
    );

  const updateMcpServer = useUpdateMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Tool filtering settings updated");
    },
    onError: notifyError,
  });

  // mcpServers.update is a full-record replace for the optional UUID
  // references, so every field has to be re-sent or it gets nulled.
  const applyGroup = (groupId: string | null) => {
    updateMcpServer.mutate({
      request: {
        updateMcpServerForm: {
          id: mcpServer.id,
          name: mcpServer.name ?? undefined,
          remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
          toolsetId: mcpServer.toolsetId ?? undefined,
          environmentId: mcpServer.environmentId ?? undefined,
          userSessionIssuerId: mcpServer.userSessionIssuerId ?? undefined,
          visibility: mcpServer.visibility,
          toolVariationsGroupId: groupId ?? undefined,
        },
      },
    });
  };

  const createGroup = useCreateGlobalToolVariationGroupMutation({
    onSuccess: async (data) => {
      await invalidateAllToolVariationGroups(queryClient, {
        refetchType: "all",
      });
      // Enabling for the first time both materializes the project-default
      // group and assigns it to this server, so filtering is actually on in a
      // single click rather than leaving the user on "Disabled".
      applyGroup(data.group.id);
    },
    onError: (error) =>
      toast.error(
        error instanceof Error ? error.message : "Failed to create tool group",
      ),
  });

  const isSaving = updateMcpServer.isPending || createGroup.isPending;
  const dirty = draft !== currentValue;
  const hasGroups = groups.length > 0;
  let enableButtonContent = <Button.Text>Enable</Button.Text>;
  if (createGroup.isPending) {
    enableButtonContent = (
      <>
        <Button.LeftIcon>
          <Loader2 className="size-4 animate-spin" />
        </Button.LeftIcon>
        <Button.Text>Enabling</Button.Text>
      </>
    );
  }

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Tool Filtering</SettingsSection.Title>
        <SettingsSection.Description>
          Filter the tools exposed by this server based on their tags. All tools
          are returned by default unless filtering is enabled and a `tags` query
          parameter is provided.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field>
            <FieldLabel htmlFor="mcp-server-tool-filtering" className="sr-only">
              Tool filtering
            </FieldLabel>
            {hasGroups ? (
              <RequireScope scope="mcp:write" level="component">
                <Select
                  value={draft}
                  disabled={isSaving}
                  onValueChange={(value) => setDraft(value)}
                >
                  <SelectTrigger
                    id="mcp-server-tool-filtering"
                    className="w-72"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={DISABLED_VALUE}>Disabled</SelectItem>
                    {groups.map((group) => (
                      <SelectItem key={group.id} value={group.id}>
                        {toolVariationsGroupDisplayName(group.name)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </RequireScope>
            ) : (
              <RequireScope scope="mcp:write" level="component">
                <Button
                  variant="secondary"
                  size="md"
                  disabled={isSaving || groupsQuery.isLoading}
                  onClick={() => createGroup.mutate({})}
                >
                  {enableButtonContent}
                </Button>
              </RequireScope>
            )}
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Filtering applies to every endpoint on this server.
          </SettingsSection.FooterHint>
          {hasGroups && (
            <SettingsSection.FooterActions>
              <RequireScope scope="mcp:write" level="component">
                <Button
                  variant="primary"
                  size="md"
                  disabled={!dirty || isSaving}
                  onClick={() =>
                    applyGroup(draft === DISABLED_VALUE ? null : draft)
                  }
                >
                  <FooterSaveButtonContent
                    pending={updateMcpServer.isPending}
                  />
                </Button>
              </RequireScope>
            </SettingsSection.FooterActions>
          )}
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

// --- Danger zone ----------------------------------------------------------

function mcpServerVisibilityUpdateForm(
  mcpServer: McpServer,
  visibility: McpServerVisibility,
) {
  return {
    id: mcpServer.id,
    name: mcpServer.name ?? undefined,
    remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
    toolsetId: mcpServer.toolsetId ?? undefined,
    environmentId: mcpServer.environmentId ?? undefined,
    userSessionIssuerId: mcpServer.userSessionIssuerId ?? undefined,
    toolVariationsGroupId: mcpServer.toolVariationsGroupId ?? undefined,
    visibility,
  };
}

function mcpServerVisibilityToast(visibility: McpServerVisibility) {
  switch (visibility) {
    case "disabled":
      return "MCP server disabled";
    case "private":
      return "MCP server enabled";
    case "public":
      return "MCP server set to public";
    default:
      return "MCP server updated";
  }
}

function ServerControlRow({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-3 py-4 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0 space-y-1">
        <Type small className="font-medium">
          {title}
        </Type>
        <Type muted small className="max-w-2xl">
          {description}
        </Type>
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </div>
  );
}

function DangerZoneCard({
  mcpServer,
  endpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
}) {
  const navigate = useNavigate();
  const routes = useRoutes();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [pendingAvailability, setPendingAvailability] =
    useState<McpServerVisibility | null>(null);
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const [isFetchingLatestMcpServer, setIsFetchingLatestMcpServer] =
    useState(false);
  const notifyVisibilityUpdateError = (error: unknown) => {
    toast.error(
      error instanceof Error ? error.message : "Failed to update MCP server",
    );
  };
  const updateVisibility = useUpdateMcpServerMutation({
    onSuccess: async (_data, variables) => {
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      const next = variables.request.updateMcpServerForm.visibility;
      toast.success(mcpServerVisibilityToast(next));
    },
    onError: notifyVisibilityUpdateError,
  });
  const isUpdatingVisibility =
    isFetchingLatestMcpServer || updateVisibility.isPending;

  const applyVisibility = async (visibility: McpServerVisibility) => {
    if (visibility === mcpServer.visibility) return;
    setIsFetchingLatestMcpServer(true);
    try {
      const latestMcpServer = await queryClient.fetchQuery({
        ...buildGetMcpServerQuery(client, { id: mcpServer.id }),
        staleTime: 0,
      });

      if (visibility === latestMcpServer.visibility) return;

      updateVisibility.mutate({
        request: {
          updateMcpServerForm: mcpServerVisibilityUpdateForm(
            latestMcpServer,
            visibility,
          ),
        },
      });
    } catch (error) {
      notifyVisibilityUpdateError(error);
    } finally {
      setIsFetchingLatestMcpServer(false);
    }
  };

  const requestAvailabilityChange = (checked: boolean) => {
    setPendingAvailability(checked ? "private" : "disabled");
  };

  const confirmAvailabilityChange = () => {
    if (!pendingAvailability) return;
    void applyVisibility(pendingAvailability);
    setPendingAvailability(null);
  };

  const enabled = mcpServer.visibility !== "disabled";

  return (
    <>
      <DangerSettingsSection>
        <DangerSettingsSection.Header>
          <DangerSettingsSection.Title>Danger Zone</DangerSettingsSection.Title>
          <DangerSettingsSection.Description>
            Manage server availability and destructive actions.
          </DangerSettingsSection.Description>
        </DangerSettingsSection.Header>
        <DangerSettingsSection.Panel>
          <DangerSettingsSection.Body>
            <div className="divide-y">
              <ServerControlRow
                title="Server Availability"
                description={
                  enabled
                    ? "This MCP server is currently serving traffic on configured URLs."
                    : "This MCP server is offline and will not serve client traffic."
                }
              >
                <Type muted small>
                  {enabled ? "Enabled" : "Disabled"}
                </Type>
                <RequireScope scope="mcp:write" level="component">
                  <Switch
                    checked={enabled}
                    disabled={isUpdatingVisibility}
                    aria-label="Enable MCP server"
                    onCheckedChange={requestAvailabilityChange}
                  />
                </RequireScope>
              </ServerControlRow>

              <ServerControlRow
                title="Delete MCP Server"
                description="Permanently remove this server and all of its endpoints. This action cannot be undone."
              >
                <RequireScope scope="mcp:write" level="component">
                  <Button
                    variant="destructive-primary"
                    size="md"
                    onClick={() => setDeleteDialogOpen(true)}
                  >
                    <Button.LeftIcon>
                      <Trash2 className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>Delete MCP server</Button.Text>
                  </Button>
                </RequireScope>
              </ServerControlRow>
            </div>
            {updateVisibility.isError && (
              <Alert variant="error" dismissible={false}>
                {updateVisibility.error.message}
              </Alert>
            )}
          </DangerSettingsSection.Body>
        </DangerSettingsSection.Panel>
      </DangerSettingsSection>
      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <Dialog.Content className="max-w-2xl!">
          <DeleteMcpServerDialogContent
            mcpServer={mcpServer}
            endpoints={endpoints}
            onClose={() => setDeleteDialogOpen(false)}
            onSuccess={() => {
              setDeleteDialogOpen(false);
              void navigate(routes.mcp.href());
            }}
          />
        </Dialog.Content>
      </Dialog>
      <ServerAvailabilityDialog
        targetVisibility={pendingAvailability}
        isLoading={isUpdatingVisibility}
        onClose={() => setPendingAvailability(null)}
        onConfirm={confirmAvailabilityChange}
      />
    </>
  );
}

function ServerAvailabilityDialog({
  targetVisibility,
  isLoading,
  onClose,
  onConfirm,
}: {
  targetVisibility: McpServerVisibility | null;
  isLoading: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const isOpen = targetVisibility != null;
  const enabling = targetVisibility !== "disabled";
  let title = "Disable MCP server?";
  let message =
    "You are about to disable this MCP server. Users will no longer be able to connect to it. Continue?";

  if (enabling) {
    title = "Enable MCP server?";
    message =
      "You are about to enable this MCP server. Users will be able to connect to it and perform tool calls. Continue?";
  }

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-w-md">
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{message}</Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Button variant="secondary" disabled={isLoading} onClick={onClose}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant={enabling ? "primary" : "destructive-primary"}
            disabled={isLoading}
            onClick={onConfirm}
          >
            {isLoading ? (
              <>
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
                <Button.Text>Saving</Button.Text>
              </>
            ) : (
              <Button.Text>Continue</Button.Text>
            )}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function DeleteMcpServerDialogContent({
  mcpServer,
  endpoints,
  onClose,
  onSuccess,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  onClose: () => void;
  onSuccess: () => void;
}) {
  const queryClient = useQueryClient();
  const remove = useDeleteMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
      ]);
      toast.success("MCP server deleted");
      onSuccess();
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to delete MCP server",
      );
    },
  });

  const handleConfirm = () => {
    remove.mutate({ request: { id: mcpServer.id } });
  };

  let deleteButtonContent = <Button.Text>Delete MCP server</Button.Text>;
  if (remove.isPending) {
    deleteButtonContent = (
      <>
        <Button.LeftIcon>
          <Loader2 className="size-4 animate-spin" />
        </Button.LeftIcon>
        <Button.Text>Deleting</Button.Text>
      </>
    );
  }

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Delete this MCP server?</Dialog.Title>
      </Dialog.Header>
      <Stack gap={3}>
        <Type>
          This will soft-delete the MCP server <strong>{mcpServer.name}</strong>{" "}
          and the following endpoints. The action cannot be undone.
        </Type>
        {endpoints.length > 0 ? (
          <ul className="list-disc pl-6">
            {endpoints.map((endpoint) => (
              <li key={endpoint.id}>
                <Type small className="font-mono">
                  {endpoint.slug}
                  {endpoint.customDomainId
                    ? " (custom domain)"
                    : " (platform-hosted)"}
                </Type>
              </li>
            ))}
          </ul>
        ) : (
          <Type muted small>
            No endpoints are currently associated with this MCP server.
          </Type>
        )}
        {remove.isError && (
          <Alert variant="error" dismissible={false}>
            {remove.error.message}
          </Alert>
        )}
        <Stack direction="horizontal" gap={2}>
          <Button
            variant="destructive-primary"
            disabled={remove.isPending}
            onClick={handleConfirm}
          >
            {deleteButtonContent}
          </Button>
          <Button
            variant="secondary"
            disabled={remove.isPending}
            onClick={onClose}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
        </Stack>
      </Stack>
    </>
  );
}
