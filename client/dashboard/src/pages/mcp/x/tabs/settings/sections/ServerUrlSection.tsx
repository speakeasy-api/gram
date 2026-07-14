import { RequireScope } from "@/components/require-scope";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Type } from "@/components/ui/type";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useCustomDomains } from "@/hooks/useToolsetUrl";
import { toastError } from "@/lib/toast-error";
import { getServerURL } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import type { CustomDomain } from "@gram/client/models/components/customdomain.js";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useDeleteMcpEndpointMutation } from "@gram/client/react-query/deleteMcpEndpoint.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useUpdateMcpEndpointMutation } from "@gram/client/react-query/updateMcpEndpoint.js";
import { Stack } from "@/components/ui/stack";
import { Button } from "@/components/ui/button";
import { useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, XIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useMcpEndpointSlugValidation } from "../../../useMcpEndpointSlugValidation";
import { RowSaveButtonContent, SettingsSection } from "../SettingsSection";

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

export function ServerUrlSection({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
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
        <InlineEmptyState
          title="No custom domains"
          description={description}
          action={
            actionLabel && onAction ? (
              <Button variant="secondary" onClick={onAction}>
                <Button.LeftIcon>
                  <Plus className="size-4" />
                </Button.LeftIcon>
                <Button.Text>{actionLabel}</Button.Text>
              </Button>
            ) : undefined
          }
        />
      );
    } else {
      customAddressEmptyState = (
        <RequireScope scope="mcp:write" level="component">
          <InlineEmptyState
            title="No custom address"
            description="Create an MCP URL on your verified custom domain."
            action={
              <Button variant="secondary" onClick={() => setAddingCustom(true)}>
                <Button.LeftIcon>
                  <Plus className="size-4" />
                </Button.LeftIcon>
                <Button.Text>Add</Button.Text>
              </Button>
            }
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
                    <InlineEmptyState
                      title="No hosted address"
                      description="Create the default Speakeasy-hosted URL for this server."
                      action={
                        <Button
                          variant="secondary"
                          onClick={() => setAddingPlatform(true)}
                        >
                          <Button.LeftIcon>
                            <Plus className="size-4" />
                          </Button.LeftIcon>
                          <Button.Text>Add</Button.Text>
                        </Button>
                      }
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
      toastError(error, "Failed to update address");
    },
  });
  const remove = useDeleteMcpEndpointMutation({
    onSuccess: async () => {
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Address removed");
    },
    onError: (error) => {
      toastError(error, "Failed to remove address");
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
        <InputGroup>
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
        <InputGroup>
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
        <InputGroup>
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
