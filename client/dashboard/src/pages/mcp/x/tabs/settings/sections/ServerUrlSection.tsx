import { RequireScope } from "@/components/require-scope";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/Field";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/InputGroup";
import { Text } from "@/components/ui/Text";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import {
  invalidateRootMcpEndpointQueries,
  patchMcpEndpointInCache,
  useRootMcpEndpointMutation,
} from "@/hooks/useRootMcpEndpoint";
import { useCustomDomains } from "@/hooks/useToolsetUrl";
import { getServerURL } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import type { CustomDomain } from "@gram/client/models/components/customdomain.js";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import { useDeleteMcpEndpointMutation } from "@gram/client/react-query/deleteMcpEndpoint.js";
import { useUpdateMcpEndpointMutation } from "@gram/client/react-query/updateMcpEndpoint.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus, SaveIcon, Trash2, XIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useMcpEndpointSlugValidation } from "../../../useMcpEndpointSlugValidation";
import { SettingsInlineEmptyState } from "../SettingsInlineEmptyState";
import { SettingsSection } from "@/components/detail/settings-section";

const ADDRESS_INPUT_GROUP_CLASSNAME = "";
const ADDRESS_SLUG_INPUT_CLASSNAME = "font-mono pl-0! font-bold";
const ADDRESS_RANDOM_SUFFIX_ALPHABET = "abcdefghijklmnopqrstuvwxyz0123456789";
const ADDRESS_RANDOM_SUFFIX_LENGTH = 5;
export const MCP_SERVER_URL_SECTION_ID = "server-url";

// The endpoint create/update forms take exactly one backend id; spreading one
// of these supplies it.
export type EndpointBackendRef =
  | { mcpServerId: string }
  | { metaMcpServerId: string };

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

export function ServerUrlSection({
  backend,
  endpoints,
  isLoadingEndpoints,
  /** What the addresses point at, for copy that reads naturally. */
  subject = "server",
}: {
  backend: EndpointBackendRef;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
  subject?: "server" | "gateway";
}): JSX.Element {
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
        <SettingsInlineEmptyState
          title="No custom domains"
          description={description}
          actionLabel={actionLabel}
          onAction={onAction}
        />
      );
    } else {
      customAddressEmptyState = (
        <RequireScope scope="mcp:write" level="component">
          <SettingsInlineEmptyState
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
        <SettingsSection.Title>
          {subject === "gateway" ? "Gateway URL" : "Server URL"}
        </SettingsSection.Title>
        <SettingsSection.Description>
          {`The web address MCP clients use to connect to this ${subject}.`}
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {isLoadingEndpoints ? (
            <Text muted small>
              Loading…
            </Text>
          ) : (
            <FieldGroup className="gap-6">
              {/* Hosted (platform) address: at most one. */}
              <Field>
                <FieldLabel>Hosted Address</FieldLabel>
                {platformEndpoint ? (
                  <AddressRow
                    backend={backend}
                    endpoint={platformEndpoint}
                    isLastEndpoint={endpoints.length === 1}
                  />
                ) : addingPlatform ? (
                  <NewPlatformAddressRow
                    backend={backend}
                    onClose={() => setAddingPlatform(false)}
                  />
                ) : (
                  <RequireScope scope="mcp:write" level="component">
                    <SettingsInlineEmptyState
                      title="No hosted address"
                      description={`Create the default Speakeasy-hosted URL for this ${subject}.`}
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
                    backend={backend}
                    endpoint={endpoint}
                    domains={availableDomains}
                    isLastEndpoint={endpoints.length === 1}
                    canManageDomainRoot={canManageDomains}
                  />
                ))}
                {addingCustom && (
                  <NewCustomAddressRow
                    backend={backend}
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

// A single editable address. The slug input is always live; Save persists the
// edit (disabled until dirty + valid) and Remove deletes immediately — except
// for the server's last address, which asks for confirmation first since it
// leaves the server unreachable and unpublishable.
function AddressRow({
  backend,
  endpoint,
  domains,
  isLastEndpoint,
  canManageDomainRoot = false,
}: {
  backend: EndpointBackendRef;
  endpoint: McpEndpoint;
  domains?: CustomDomain[];
  isLastEndpoint: boolean;
  canManageDomainRoot?: boolean;
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
    onSuccess: async (updated) => {
      patchMcpEndpointInCache(queryClient, updated);
      await invalidateRootMcpEndpointQueries(queryClient);
      toast.success("Address updated");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to update address",
      );
    },
  });
  const [confirmRemoveOpen, setConfirmRemoveOpen] = useState(false);
  const remove = useDeleteMcpEndpointMutation({
    onSuccess: async () => {
      setConfirmRemoveOpen(false);
      await invalidateRootMcpEndpointQueries(queryClient);
      toast.success("Address removed");
    },
    onError: (error) => {
      setConfirmRemoveOpen(false);
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
  const rootMutation = useRootMcpEndpointMutation();
  const [confirmRootOpen, setConfirmRootOpen] = useState(false);

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
          ...backend,
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
            aria-label={update.isPending ? "Saving address" : "Save address"}
          >
            <Button.Icon>
              {update.isPending ? (
                <Loader2 aria-hidden="true" className="size-4 animate-spin" />
              ) : (
                <SaveIcon aria-hidden="true" className="size-4" />
              )}
            </Button.Icon>
          </Button>
          <Button
            variant="destructive-secondary"
            className="border border-destructive/40 bg-transparent hover:border-destructive"
            disabled={remove.isPending}
            onClick={() => {
              if (isLastEndpoint) {
                setConfirmRemoveOpen(true);
                return;
              }
              remove.mutate({ request: { id: endpoint.id } });
            }}
          >
            <Button.LeftIcon>
              <Trash2 className="size-4" />
            </Button.LeftIcon>
          </Button>
        </RequireScope>
      </Stack>
      {slugError && <FieldError className="text-xs">{slugError}</FieldError>}
      {update.isError && <FieldError>{update.error.message}</FieldError>}
      {endpoint.customDomainId && (
        <div className="flex flex-wrap items-center gap-2">
          {endpoint.isDomainRoot && (
            <Badge variant="success" background>
              Domain root
            </Badge>
          )}
          <Button
            variant="tertiary"
            size="sm"
            disabled={!canManageDomainRoot || rootMutation.isPending}
            title={
              canManageDomainRoot
                ? undefined
                : "Organization admin permission is required"
            }
            onClick={() => setConfirmRootOpen(true)}
          >
            <Button.Text>
              {endpoint.isDomainRoot ? "Clear root" : "Set as domain root"}
            </Button.Text>
          </Button>
        </div>
      )}
      <ConfirmDomainRootDialog
        isOpen={confirmRootOpen}
        isClear={!!endpoint.isDomainRoot}
        domainLabel={customDomainLabel || "your custom domain"}
        onClose={() => setConfirmRootOpen(false)}
        onConfirm={() => {
          setConfirmRootOpen(false);
          if (endpoint.customDomainId) {
            rootMutation.setRootMcpEndpoint(
              endpoint.customDomainId,
              endpoint.isDomainRoot ? undefined : endpoint.id,
            );
          }
        }}
      />
      <RemoveLastAddressDialog
        isOpen={confirmRemoveOpen}
        isLoading={remove.isPending}
        onClose={() => setConfirmRemoveOpen(false)}
        onConfirm={() => remove.mutate({ request: { id: endpoint.id } })}
      />
    </Field>
  );
}

// Root mapping changes reroute live traffic on the custom domain, so both
// setting (which replaces any existing root mapping) and clearing require
// explicit confirmation, mirroring the last-address removal dialog.
function ConfirmDomainRootDialog({
  isOpen,
  isClear,
  domainLabel,
  onClose,
  onConfirm,
}: {
  isOpen: boolean;
  isClear: boolean;
  domainLabel: string;
  onClose: () => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-w-md">
        <Dialog.Header>
          <Dialog.Title>
            {isClear
              ? "Clear the domain root mapping?"
              : "Route the domain root to this server?"}
          </Dialog.Title>
          <Dialog.Description>
            {isClear
              ? `Requests to https://${domainLabel}/ will stop routing to this MCP server. Its /mcp/ addresses keep working.`
              : `Requests to https://${domainLabel}/ will be served by this MCP server, replacing any existing root mapping on the domain.`}
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Button variant="secondary" onClick={onClose}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant={isClear ? "destructive-primary" : "primary"}
            onClick={onConfirm}
          >
            <Button.Text>{isClear ? "Clear root" : "Set as root"}</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

// Confirmation for removing a server's only address. Without an address the
// server can't serve traffic — it can't be added to plugins until a new
// address is created.
function RemoveLastAddressDialog({
  isOpen,
  isLoading,
  onClose,
  onConfirm,
}: {
  isOpen: boolean;
  isLoading: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-w-md">
        <Dialog.Header>
          <Dialog.Title>Remove this server's only address?</Dialog.Title>
          <Dialog.Description>
            This is the last address for this MCP server. Removing it means
            clients can no longer connect, and the server can't be added to
            plugins until you create a new address.
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Button variant="secondary" disabled={isLoading} onClick={onClose}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="destructive-primary"
            disabled={isLoading}
            onClick={onConfirm}
          >
            {isLoading && (
              <Button.LeftIcon>
                <Loader2 aria-hidden="true" className="size-4 animate-spin" />
              </Button.LeftIcon>
            )}
            <Button.Text>
              {isLoading ? "Removing" : "Remove address"}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function NewPlatformAddressRow({
  backend,
  onClose,
}: {
  backend: EndpointBackendRef;
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
          ...backend,
          slug: fullSlug,
        },
      });
      await invalidateRootMcpEndpointQueries(queryClient);
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
          aria-label={submitting ? "Adding address" : "Add address"}
        >
          <Button.Icon>
            {submitting ? (
              <Loader2 aria-hidden="true" className="size-4 animate-spin" />
            ) : (
              <SaveIcon aria-hidden="true" className="size-4" />
            )}
          </Button.Icon>
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
  backend,
  domains,
  onClose,
}: {
  backend: EndpointBackendRef;
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
          ...backend,
          slug: trimmed,
          customDomainId: domainId,
        },
      });
      await invalidateRootMcpEndpointQueries(queryClient);
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
          aria-label={submitting ? "Adding address" : "Add address"}
        >
          <Button.Icon>
            {submitting ? (
              <Loader2 aria-hidden="true" className="size-4 animate-spin" />
            ) : (
              <SaveIcon aria-hidden="true" className="size-4" />
            )}
          </Button.Icon>
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
