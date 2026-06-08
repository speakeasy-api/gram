import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useProductTier } from "@/hooks/useProductTier";
import {
  customDomainMcpEndpointUrl,
  useCustomDomain,
} from "@/hooks/useToolsetUrl";
import { HumanizeDateTime } from "@/lib/dates";
import { cn, getCustomDomainCNAME } from "@/lib/utils";
import { useCustomDomainMcpEndpoints } from "@gram/client/react-query/customDomainMcpEndpoints";
import { useDeleteDomainMutation } from "@gram/client/react-query/deleteDomain";
import { invalidateAllGetDomain } from "@gram/client/react-query/getDomain";
import { useRegisterDomainMutation } from "@gram/client/react-query/registerDomain";
import { useUpdateDomainMutation } from "@gram/client/react-query/updateDomain";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  Check,
  CheckCircle2,
  ChevronRight,
  Copy,
  Globe,
  Loader2,
  Trash2,
  X,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { RequireScope } from "@/components/require-scope";

export default function OrgDomains() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <OrgDomainsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function validateIPEntry(entry: string): string {
  const trimmed = entry.trim();
  if (!trimmed) return "Entry is required";

  // CIDR notation
  const cidrRegex = /^(\d{1,3}\.){3}\d{1,3}\/(\d|[1-2]\d|3[0-2])$/;
  if (cidrRegex.test(trimmed)) {
    const octets = trimmed.split("/")[0].split(".").map(Number);
    if (octets.every((o) => o >= 0 && o <= 255)) return "";
    return "Octet out of range (0–255)";
  }

  // Plain IP
  const ipRegex = /^(\d{1,3}\.){3}\d{1,3}$/;
  if (ipRegex.test(trimmed)) {
    const octets = trimmed.split(".").map(Number);
    if (octets.every((o) => o >= 0 && o <= 255)) return "";
    return "Octet out of range (0–255)";
  }

  return "Enter a valid IP address (1.2.3.4) or CIDR range (10.0.0.0/24)";
}

type IPRow = { id: number; value: string; error: string | null };

// Inline editor: each allowlist entry is its own editable field. Entries are
// validated on blur (and on save, by the parent via `onValidityChange`) rather
// than gated behind explicit add/remove actions.
function IPAllowlistEditor({
  ips,
  onIpsChange,
  onValidityChange,
}: {
  ips: string[];
  onIpsChange: (ips: string[]) => void;
  onValidityChange?: (valid: boolean) => void;
}) {
  // Local row state preserves in-progress (possibly invalid or duplicate)
  // entries while editing; the parent only ever receives cleaned values. Each
  // row carries a stable `id` so React keys survive reordering/removal.
  const nextId = useRef(0);
  const makeRow = (value: string): IPRow => ({
    id: nextId.current++,
    value,
    error: null,
  });
  const [rows, setRows] = useState<IPRow[]>(() =>
    (ips.length > 0 ? ips : [""]).map(makeRow),
  );

  function commit(next: IPRow[]) {
    const trimmed = next.map((r) => r.value.trim());
    const valid = trimmed.every((r) => r === "" || validateIPEntry(r) === "");
    const cleaned = Array.from(new Set(trimmed.filter((r) => r !== "")));
    onIpsChange(cleaned);
    onValidityChange?.(valid);
  }

  function handleChange(id: number, value: string) {
    const next = rows.map((r) =>
      r.id === id ? { ...r, value, error: null } : r,
    );
    setRows(next);
    commit(next);
  }

  function handleBlur(id: number) {
    setRows((prev) =>
      prev.map((r) => {
        if (r.id !== id) return r;
        const value = r.value.trim();
        return { ...r, error: value ? validateIPEntry(value) || null : null };
      }),
    );
  }

  function handleRemove(id: number) {
    const filtered = rows.filter((r) => r.id !== id);
    const next = filtered.length > 0 ? filtered : [makeRow("")];
    setRows(next);
    commit(next);
  }

  function handleAddRow() {
    setRows([...rows, makeRow("")]);
  }

  return (
    <div className="space-y-2">
      {rows.map((row) => (
        <div key={row.id} className="space-y-1">
          <div className="flex items-center gap-2">
            <Input
              placeholder="1.2.3.4 or 10.0.0.0/24"
              value={row.value}
              onChange={(val) => handleChange(row.id, val)}
              onBlur={() => handleBlur(row.id)}
              className={cn("font-mono", row.error && "border-destructive")}
            />
            <Button
              variant="tertiary"
              size="sm"
              className="hover:text-destructive shrink-0"
              onClick={() => handleRemove(row.id)}
              aria-label="Remove entry"
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
          {row.error && (
            <Type variant="body" className="text-destructive text-xs">
              {row.error}
            </Type>
          )}
        </div>
      ))}
      <Button variant="tertiary" size="sm" onClick={handleAddRow}>
        + Add IP address
      </Button>
    </div>
  );
}

export function OrgDomainsInner() {
  const organization = useOrganization();
  const productTier = useProductTier();
  const queryClient = useQueryClient();
  const [isAddDomainDialogOpen, setIsAddDomainDialogOpen] = useState(false);
  const [isCnameCopied, setIsCnameCopied] = useState(false);
  const [isTxtCopied, setIsTxtCopied] = useState(false);
  const [isCustomDomainModalOpen, setIsCustomDomainUpgradeModalOpen] =
    useState(false);
  const [isDeleteDomainDialogOpen, setIsDeleteDomainDialogOpen] =
    useState(false);
  const [domainInput, setDomainInput] = useState("");
  const [domainError, setDomainError] = useState("");
  const CNAME_VALUE = getCustomDomainCNAME();

  // IP allowlist state for create dialog
  const [pendingIPs, setPendingIPs] = useState<string[]>([]);
  const [pendingIPsValid, setPendingIPsValid] = useState(true);
  const [isAllowlistExpanded, setIsAllowlistExpanded] = useState(false);

  // Edit allowlist side panel state
  const [isEditAllowlistOpen, setIsEditAllowlistOpen] = useState(false);
  const [editIPs, setEditIPs] = useState<string[]>([]);
  const [editIPsValid, setEditIPsValid] = useState(true);
  const [updateAllowlistError, setUpdateAllowlistError] = useState("");

  const domainRegex = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z]{2,})+$/i;

  const validDomain =
    domainInput.trim() && domainRegex.test(domainInput.trim());
  const subdomain = validDomain ? domainInput.trim() : "sub.yourdomain.com";
  const txtName = `_gram.${subdomain}`;
  const txtValue = `gram-domain-verify=${subdomain},${organization.id}`;

  const {
    domain,
    isLoading: domainIsLoading,
    refetch: domainRefetch,
  } = useCustomDomain();

  useEffect(() => {
    if (domain?.domain && !domainInput) {
      setDomainInput(domain.domain);
    }
  }, [domain?.domain, domainInput]);

  const validateDomain = (domain: string): string => {
    if (!domain.trim()) {
      return "Domain is required";
    }
    if (!domainRegex.test(domain)) {
      return "Please enter a valid domain name";
    }
    return "";
  };

  const handleCopyCname = async () => {
    await navigator.clipboard.writeText(CNAME_VALUE);
    setIsCnameCopied(true);
    setTimeout(() => setIsCnameCopied(false), 2000);
  };
  const handleCopyTxt = async () => {
    await navigator.clipboard.writeText(txtValue);
    setIsTxtCopied(true);
    setTimeout(() => setIsTxtCopied(false), 2000);
  };

  const registerDomainMutation = useRegisterDomainMutation({
    onSuccess: () => {
      setIsAddDomainDialogOpen(false);
      setDomainInput("");
      setDomainError("");
      setPendingIPs([]);
      setPendingIPsValid(true);
      setIsAllowlistExpanded(false);
      setTimeout(() => {
        domainRefetch();
      }, 2000);
    },
    onError: (error) => {
      setDomainError(error.message || "Failed to register domain");
    },
  });

  const deleteDomainMutation = useDeleteDomainMutation({
    onSuccess: async () => {
      setIsDeleteDomainDialogOpen(false);
      setDomainInput("");
      await invalidateAllGetDomain(queryClient);
    },
  });

  const updateDomainMutation = useUpdateDomainMutation({
    onSuccess: async () => {
      setIsEditAllowlistOpen(false);
      await invalidateAllGetDomain(queryClient);
    },
    onError: (error) => {
      setUpdateAllowlistError(error.message || "Failed to save allowlist");
    },
  });

  // Preview which MCP endpoints will be cascaded by the delete. Only fetched
  // while the confirmation dialog is open and a domain is configured.
  const impactQuery = useCustomDomainMcpEndpoints(undefined, undefined, {
    enabled: isDeleteDomainDialogOpen && Boolean(domain?.domain),
  });
  const impactedEndpoints = impactQuery.data?.mcpEndpoints ?? [];

  const handleDomainInputChange = (value: string) => {
    setDomainInput(value);
    setDomainError(validateDomain(value));
  };

  const handleRegisterDomain = () => {
    const error = validateDomain(domainInput);
    if (error) {
      setDomainError(error);
      return;
    }

    registerDomainMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        createDomainRequestBody: {
          domain: domainInput.trim(),
          ipAllowlist: pendingIPs.length > 0 ? pendingIPs : undefined,
        },
      },
    });
  };

  const handleSaveAllowlist = () => {
    updateDomainMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        updateDomainRequestBody: {
          ipAllowlist: editIPs,
        },
      },
    });
  };

  useEffect(() => {
    if (!domain?.isUpdating) return;
    const interval = setInterval(() => {
      domainRefetch();
    }, 30000);
    return () => clearInterval(interval);
  }, [domain?.isUpdating, domainRefetch]);

  return (
    <>
      <Heading variant="h4" className="mb-2">
        Custom Domain
      </Heading>
      <Type muted small className="mb-6">
        Connect a custom domain to serve your MCP servers from your own branded
        URL instead of the default platform domain.
      </Type>
      {domain?.domain ? (
        <div className="border-border bg-card rounded-lg border p-4">
          <Stack direction="horizontal" justify="space-between" align="start">
            <Stack gap={1}>
              <Stack direction="horizontal" align="center" gap={2}>
                <Globe className="text-muted-foreground h-4 w-4" />
                <Type variant="body" className="font-mono font-medium">
                  {domain.domain}
                </Type>
                {domain.isUpdating ? (
                  <SimpleTooltip tooltip="Your domain is being verified. This may take a few minutes.">
                    <Loader2 className="h-4 w-4 animate-spin text-blue-500" />
                  </SimpleTooltip>
                ) : domain.verified ? (
                  <SimpleTooltip tooltip="Domain verified and active">
                    <Check className="h-4 w-4 stroke-3 text-green-500" />
                  </SimpleTooltip>
                ) : (
                  <SimpleTooltip tooltip="Domain verification failed. Ensure your DNS records are set up correctly.">
                    <X className="h-4 w-4 stroke-3 text-red-500" />
                  </SimpleTooltip>
                )}
              </Stack>
              <Type
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Linked <HumanizeDateTime date={domain.createdAt} />
              </Type>
              <div className="mt-1 ml-6 flex flex-wrap items-center gap-2">
                <Type variant="body" className="text-muted-foreground text-sm">
                  Allowed IPs:
                </Type>
                {domain.ipAllowlist.length === 0 ? (
                  <Type
                    variant="body"
                    className="text-muted-foreground text-sm italic"
                  >
                    All (no restriction)
                  </Type>
                ) : (
                  domain.ipAllowlist.map((ip) => (
                    <Badge key={ip} variant="secondary" className="font-mono">
                      {ip}
                    </Badge>
                  ))
                )}
              </div>
            </Stack>
            <RequireScope scope="org:admin" level="section">
              <Stack direction="horizontal" gap={2}>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => {
                    setEditIPs(domain.ipAllowlist);
                    setEditIPsValid(true);
                    setUpdateAllowlistError("");
                    setIsEditAllowlistOpen(true);
                  }}
                >
                  Edit allowlist
                </Button>
                {!domain.verified && (
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => setIsAddDomainDialogOpen(true)}
                    disabled={domain.isUpdating}
                  >
                    Reverify
                  </Button>
                )}
                <Button
                  variant="tertiary"
                  size="sm"
                  onClick={() => setIsDeleteDomainDialogOpen(true)}
                  className="hover:text-destructive"
                  disabled={deleteDomainMutation.isPending}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </Stack>
            </RequireScope>
          </Stack>
        </div>
      ) : (
        !domainIsLoading && (
          <div className="border-border rounded-lg border border-dashed p-6">
            <Stack gap={2} align="center" justify="center">
              <Type variant="body" className="text-muted-foreground">
                No custom domain configured
              </Type>
              <Type variant="body" className="text-muted-foreground text-sm">
                You can connect one custom domain per organization for your MCP
                servers.
              </Type>
              <RequireScope scope="org:admin" level="component">
                <Button
                  size="sm"
                  variant="secondary"
                  className="mt-2"
                  onClick={() => {
                    if (productTier.includes("base")) {
                      setIsCustomDomainUpgradeModalOpen(true);
                    } else {
                      setIsAddDomainDialogOpen(true);
                    }
                  }}
                >
                  <Button.LeftIcon>
                    <Globe className="h-4 w-4" />
                  </Button.LeftIcon>
                  <Button.Text>Add Domain</Button.Text>
                </Button>
              </RequireScope>
            </Stack>
          </div>
        )
      )}

      <Dialog
        open={isDeleteDomainDialogOpen}
        onOpenChange={setIsDeleteDomainDialogOpen}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Remove Custom Domain</Dialog.Title>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            <Type variant="body">
              Are you sure you want to remove{" "}
              <span className="font-bold italic">{domain?.domain}</span>? This
              will delete the associated ingress and TLS certificate.
            </Type>
            {impactQuery.isLoading ? (
              <Type variant="small" muted>
                Checking for MCP endpoints under this domain&hellip;
              </Type>
            ) : impactedEndpoints.length > 0 ? (
              <div className="space-y-2">
                <Type variant="body" className="font-semibold">
                  {impactedEndpoints.length === 1
                    ? "1 MCP endpoint will be deactivated:"
                    : `${impactedEndpoints.length} MCP endpoints will be deactivated:`}
                </Type>
                <ul className="border-border max-h-48 list-disc space-y-1 overflow-y-auto rounded-md border px-6 py-2">
                  {impactedEndpoints.map((endpoint) => (
                    <li key={endpoint.id}>
                      <Type variant="small">
                        <span className="font-mono">
                          {domain?.domain
                            ? customDomainMcpEndpointUrl(
                                domain.domain,
                                endpoint.slug,
                              )
                            : endpoint.slug}
                        </span>{" "}
                        <Type variant="small" as="span" muted>
                          &middot; {endpoint.projectName} &middot;{" "}
                          {endpoint.mcpServerName ??
                            endpoint.mcpServerSlug ??
                            endpoint.mcpServerId}
                        </Type>
                      </Type>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
            <div className="flex justify-end space-x-2">
              <Button
                variant="secondary"
                onClick={() => setIsDeleteDomainDialogOpen(false)}
              >
                Cancel
              </Button>
              <RequireScope scope="org:admin" level="component">
                <Button
                  variant="destructive-primary"
                  onClick={() =>
                    deleteDomainMutation.mutate({
                      security: { sessionHeaderGramSession: "" },
                    })
                  }
                  disabled={deleteDomainMutation.isPending}
                >
                  {deleteDomainMutation.isPending ? "Removing..." : "Remove"}
                </Button>
              </RequireScope>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>

      <Dialog
        open={isAddDomainDialogOpen}
        onOpenChange={(open) => {
          setIsAddDomainDialogOpen(open);
          if (!open) {
            setPendingIPs([]);
            setPendingIPsValid(true);
            setIsAllowlistExpanded(false);
          }
        }}
      >
        <Dialog.Content className="max-w-lg">
          <Dialog.Header>
            <Dialog.Title>Connect a Custom Domain</Dialog.Title>
          </Dialog.Header>
          <div className="min-h-[420px] space-y-6 py-4">
            <div>
              <Type
                variant="body"
                className="mb-2 block text-lg font-extrabold"
              >
                Step 1
              </Type>
              <Type variant="body" className="text-muted-foreground mb-2">
                Enter your custom domain:
              </Type>
              <div className="space-y-2">
                <Input
                  placeholder="Enter your domain (chat.yourdomain.com)"
                  value={domainInput}
                  onChange={handleDomainInputChange}
                  className={cn(
                    domainError && "border-red-500",
                    domain?.domain &&
                      "bg-muted text-muted-foreground cursor-not-allowed",
                  )}
                  readOnly={!!domain?.domain}
                />
                {domainError && (
                  <Type variant="body" className="text-sm text-red-500">
                    {domainError}
                  </Type>
                )}
              </div>
            </div>
            <div>
              <Type
                variant="body"
                className="mb-2 block text-lg font-extrabold"
              >
                Step 2
              </Type>
              <Type variant="body" className="text-muted-foreground mb-2">
                Create a CNAME record for{" "}
                <span className="font-mono break-all">{subdomain}</span>{" "}
                pointing to the following:
              </Type>
              <div className="bg-muted mt-2 flex items-center space-x-2 rounded-md p-3">
                <code className="flex-1 break-all">{CNAME_VALUE}</code>
                <Button
                  variant="tertiary"
                  size="sm"
                  onClick={handleCopyCname}
                  className="shrink-0"
                >
                  {isCnameCopied ? (
                    <CheckCircle2 className="h-4 w-4 text-green-500" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                </Button>
              </div>
            </div>
            <div>
              <Type
                variant="body"
                className="mb-2 block text-lg font-extrabold"
              >
                Step 3
              </Type>
              <Type variant="body" className="text-muted-foreground mb-2">
                Create a TXT record at{" "}
                <span className="font-mono break-all">{txtName}</span> with the
                following value:
              </Type>
              <div className="bg-muted mt-2 flex items-center space-x-2 rounded-md p-3">
                <code className="flex-1 break-all">{txtValue}</code>
                <Button
                  variant="tertiary"
                  size="sm"
                  onClick={handleCopyTxt}
                  className="shrink-0"
                >
                  {isTxtCopied ? (
                    <CheckCircle2 className="h-4 w-4 text-green-500" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                </Button>
              </div>
            </div>
            <div>
              <button
                type="button"
                className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm"
                onClick={() => setIsAllowlistExpanded((prev) => !prev)}
              >
                <ChevronRight
                  className={cn(
                    "h-4 w-4 transition-transform",
                    isAllowlistExpanded && "rotate-90",
                  )}
                />
                Advanced: IP allowlist (optional)
              </button>
              {isAllowlistExpanded && (
                <div className="mt-3 pl-5">
                  <Type
                    variant="body"
                    className="text-muted-foreground mb-3 text-sm"
                  >
                    Restrict access to specific IP addresses or CIDR ranges.
                    Leave empty to allow all traffic.
                  </Type>
                  <IPAllowlistEditor
                    ips={pendingIPs}
                    onIpsChange={setPendingIPs}
                    onValidityChange={setPendingIPsValid}
                  />
                </div>
              )}
            </div>
            <div className="mt-4 flex justify-end">
              <RequireScope scope="org:admin" level="component">
                <Button
                  onClick={handleRegisterDomain}
                  disabled={
                    !domainInput.trim() ||
                    !!domainError ||
                    !pendingIPsValid ||
                    registerDomainMutation.isPending
                  }
                >
                  {registerDomainMutation.isPending
                    ? "Registering..."
                    : domain?.domain
                      ? "Reverify"
                      : "Register"}
                </Button>
              </RequireScope>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>

      <Sheet open={isEditAllowlistOpen} onOpenChange={setIsEditAllowlistOpen}>
        <SheetContent side="right" className="w-full sm:max-w-md">
          <SheetHeader>
            <SheetTitle>Edit IP allowlist</SheetTitle>
            <SheetDescription>
              Restrict access to{" "}
              <span className="font-mono">{domain?.domain}</span> to specific IP
              addresses or CIDR ranges. Leave empty to allow all traffic.
            </SheetDescription>
          </SheetHeader>
          <div className="flex-1 space-y-4 overflow-y-auto px-4">
            <IPAllowlistEditor
              ips={editIPs}
              onIpsChange={setEditIPs}
              onValidityChange={setEditIPsValid}
            />
            {updateAllowlistError && (
              <Type variant="body" className="text-destructive text-sm">
                {updateAllowlistError}
              </Type>
            )}
          </div>
          <SheetFooter className="flex-row justify-end gap-2">
            <Button
              variant="secondary"
              onClick={() => {
                setIsEditAllowlistOpen(false);
                setUpdateAllowlistError("");
              }}
              disabled={updateDomainMutation.isPending}
            >
              Cancel
            </Button>
            <RequireScope scope="org:admin" level="component">
              <Button
                onClick={handleSaveAllowlist}
                disabled={!editIPsValid || updateDomainMutation.isPending}
              >
                {updateDomainMutation.isPending ? "Saving..." : "Save"}
              </Button>
            </RequireScope>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <FeatureRequestModal
        isOpen={isCustomDomainModalOpen}
        onClose={() => setIsCustomDomainUpgradeModalOpen(false)}
        title="Custom Domains"
        description="Custom domains require upgrading to an enterprise plan. Someone should be in touch shortly, or feel free to book a meeting directly."
        actionType="custom_domain"
        icon={Globe}
        accountUpgrade
      />
    </>
  );
}
