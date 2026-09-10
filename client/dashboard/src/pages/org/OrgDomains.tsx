import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { SettingsPage } from "@/components/page-templates";
import { Badge } from "@/components/ui/Badge";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useProductTier } from "@/hooks/useProductTier";
import { useRBAC } from "@/hooks/useRBAC";
import { useRootMcpEndpointMutation } from "@/hooks/useRootMcpEndpoint";
import {
  customDomainMcpEndpointUrl,
  useCustomDomain,
} from "@/hooks/useToolsetUrl";
import { HumanizeDateTime } from "@/lib/dates";
import { cn, getCustomDomainCNAME } from "@/lib/utils";
import type { CustomDomain } from "@gram/client/models/components/customdomain.js";
import type { CustomDomainMcpEndpoint } from "@gram/client/models/components/customdomainmcpendpoint.js";
import type { RootMcpServerOption } from "@gram/client/models/components/rootmcpserveroption.js";
import { useCustomDomainMcpEndpoints } from "@gram/client/react-query/customDomainMcpEndpoints";
import { useRootMcpServers } from "@gram/client/react-query/rootMcpServers";
import { useCheckDomainHealthMutation } from "@gram/client/react-query/checkDomainHealth";
import { useDeleteDomainMutation } from "@gram/client/react-query/deleteDomain";
import { invalidateAllGetDomain } from "@gram/client/react-query/getDomain";
import { invalidateAllListDomains } from "@gram/client/react-query/listDomains";
import { useRegisterDomainMutation } from "@gram/client/react-query/registerDomain";
import { useUpdateDomainMutation } from "@gram/client/react-query/updateDomain";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import {
  Check,
  CheckCircle2,
  ChevronRight,
  Copy,
  Globe,
  AlertTriangle,
  Loader2,
  Trash2,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { RequireScope } from "@/components/require-scope";
import { toast } from "sonner";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";

export default function OrgDomains(): JSX.Element {
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <OrgDomainsInner />
    </RequireScope>
  );
}

function validateIPEntry(entry: string): string {
  const trimmed = entry.trim();
  if (!trimmed) return "Entry is required";

  // CIDR notation
  const cidrRegex = /^(\d{1,3}\.){3}\d{1,3}\/(\d|[1-2]\d|3[0-2])$/;
  if (cidrRegex.test(trimmed)) {
    const octets = trimmed.split("/")[0]!.split(".").map(Number);
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

const healthIssueMessages: Record<string, string> = {
  dns_not_found:
    "We couldn't find DNS records for this domain. Set this record with your DNS provider:",
  dns_target_mismatch:
    "This domain's DNS does not resolve to the expected target. If the domain sits behind a proxy or CDN, traffic may still work; otherwise set this DNS record:",
  caa_forbidden:
    "CAA records for this domain do not allow Let's Encrypt to issue a TLS certificate. Add this CAA record:",
  resource_missing:
    "The routing configuration for this domain is missing. Run the check again to confirm the problem persists.",
  certificate_missing:
    "The TLS certificate for this domain is missing. Run the check again to confirm the problem persists.",
  certificate_not_ready:
    "The TLS certificate for this domain is not ready. Check your DNS configuration, then run the check again.",
  certificate_expired:
    "The TLS certificate for this domain has expired. Check your DNS configuration, then run the check again.",
  certificate_invalid:
    "The TLS certificate does not match this domain or could not be read. Run the check again to confirm the problem persists.",
  check_failed:
    "We couldn't complete the latest health check. Run it again to confirm whether the domain is healthy.",
};

const LETS_ENCRYPT_CAA_RECORD = '0 issue "letsencrypt.org"';

function customDomainCAARecord(domainName: string): string {
  return `${domainName} CAA ${LETS_ENCRYPT_CAA_RECORD}`;
}

function customDomainHealthMessage(issue?: string): string {
  return issue
    ? (healthIssueMessages[issue] ??
        "The latest health check found a problem with this domain.")
    : "The latest health check found a problem with this domain.";
}

// DNS-shaped issues end in a colon and expect the exact record the customer
// must create; the other messages stand alone.
function CustomDomainHealthMessage({
  issue,
  domainName,
  recordType,
  aRecords,
  cnameTarget,
}: {
  issue?: string;
  domainName: string;
  recordType?: string;
  aRecords?: string[];
  cnameTarget?: string;
}) {
  const showsExpectedRecord =
    issue === "dns_not_found" || issue === "dns_target_mismatch";
  const showsCAARecord = issue === "caa_forbidden";
  const useARecord = recordType === "a" && (aRecords?.length ?? 0) > 0;
  return (
    <>
      {customDomainHealthMessage(issue)}
      {showsExpectedRecord && (
        <>
          {" "}
          <code className="break-all">
            {useARecord
              ? `${domainName} A ${aRecords?.join(", ")}`
              : `${domainName} CNAME ${cnameTarget || getCustomDomainCNAME()}`}
          </code>
        </>
      )}
      {showsCAARecord && (
        <>
          {" "}
          <code className="break-all">{customDomainCAARecord(domainName)}</code>
        </>
      )}
    </>
  );
}

// A single failed probe (check_failed) is usually a transient Gram-side issue,
// not a customer-actionable problem; only surface it once it has persisted
// across consecutive checks.
function showCustomDomainUnhealthy(domain: {
  verified: boolean;
  healthStatus?: string;
  healthIssue?: string;
  consecutiveFailures?: number;
}): boolean {
  if (!domain.verified || domain.healthStatus !== "unhealthy") {
    return false;
  }
  return (
    domain.healthIssue !== "check_failed" ||
    (domain.consecutiveFailures ?? 0) >= 2
  );
}

// Unhealthy state on an unverified domain means the health sweep auto-disabled
// it: routing was torn down and it must go back through the reverify flow.
function showCustomDomainAutoDisabled(domain: {
  verified: boolean;
  healthStatus?: string;
  unhealthySince?: unknown;
}): boolean {
  return (
    !domain.verified &&
    domain.healthStatus === "unhealthy" &&
    Boolean(domain.unhealthySince)
  );
}

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
              <Button.Icon>
                <X className="h-4 w-4" />
              </Button.Icon>
            </Button>
          </div>
          {row.error && (
            <Text variant="body" className="text-destructive text-xs">
              {row.error}
            </Text>
          )}
        </div>
      ))}
      <Button variant="tertiary" size="sm" onClick={handleAddRow}>
        + Add IP address
      </Button>
    </div>
  );
}

const NO_ROOT_MCP_ENDPOINT = "__none__";

function mcpServerLabel(endpoint: CustomDomainMcpEndpoint): string {
  return (
    endpoint.mcpServerName ??
    endpoint.mcpServerSlug ??
    endpoint.mcpServerId ??
    endpoint.metaMcpServerId ??
    endpoint.id
  );
}

function rootServerLabel(option: RootMcpServerOption): string {
  return option.name ?? option.slug ?? option.mcpServerId;
}

function groupRootServerOptions(options: RootMcpServerOption[]): {
  projectId: string;
  projectName: string;
  servers: RootMcpServerOption[];
}[] {
  const groups = new Map<
    string,
    { projectName: string; servers: RootMcpServerOption[] }
  >();

  for (const option of options) {
    const group = groups.get(option.projectId);
    if (group) {
      group.servers.push(option);
    } else {
      groups.set(option.projectId, {
        projectName: option.projectName,
        servers: [option],
      });
    }
  }

  return Array.from(groups.entries())
    .map(([projectId, group]) => ({
      projectId,
      projectName: group.projectName,
      servers: [...group.servers].sort((a, b) =>
        rootServerLabel(a).localeCompare(rootServerLabel(b)),
      ),
    }))
    .sort((a, b) => a.projectName.localeCompare(b.projectName));
}

function DefaultMcpServerControl({
  domain,
  canManage,
}: {
  domain: CustomDomain;
  canManage: boolean;
}) {
  const rootMutation = useRootMcpEndpointMutation();
  const serversQuery = useRootMcpServers(undefined, undefined, {
    refetchOnWindowFocus: false,
  });
  const servers = useMemo(
    () => serversQuery.data?.mcpServers ?? [],
    [serversQuery.data],
  );
  const serverGroups = useMemo(
    () => groupRootServerOptions(servers),
    [servers],
  );
  const currentServer = servers.find((option) => option.isDomainRoot);
  const selectedValue = currentServer?.mcpServerId ?? NO_ROOT_MCP_ENDPOINT;

  let content: React.ReactNode;
  if (serversQuery.isLoading) {
    content = (
      <Text variant="body" className="text-muted-foreground text-sm">
        Loading MCP servers…
      </Text>
    );
  } else if (servers.length === 0) {
    content = (
      <div className="border-border border border-dashed p-4">
        <Text variant="body" className="font-medium">
          No MCP servers in this organization
        </Text>
        <Text
          variant="body"
          className="text-muted-foreground mt-1 max-w-[65ch] text-sm"
        >
          Create an MCP server first, then choose it here to serve the domain
          root.
        </Text>
      </div>
    );
  } else {
    const selectedLabel = currentServer
      ? currentServer.attachedEndpointSlug
        ? `${rootServerLabel(currentServer)} · /mcp/${currentServer.attachedEndpointSlug}`
        : rootServerLabel(currentServer)
      : "No root mapping";

    content = (
      <div className="space-y-3">
        <Select
          value={selectedValue}
          disabled={!canManage || rootMutation.isPending}
          onValueChange={(value) => {
            if (value === NO_ROOT_MCP_ENDPOINT) {
              rootMutation.setRootMcpEndpoint(domain.id, undefined);
            } else {
              rootMutation.setRootMcpServer(domain.id, value);
            }
          }}
        >
          <SelectTrigger className="w-full max-w-xl">
            <SelectValue>{selectedLabel}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              value={NO_ROOT_MCP_ENDPOINT}
              description="Do not route the custom-domain root to an MCP server."
            >
              No root mapping
            </SelectItem>
            {serverGroups.map((group) => (
              <SelectGroup key={group.projectId}>
                <SelectLabel>{group.projectName}</SelectLabel>
                {group.servers.map((option) => (
                  <SelectItem
                    key={option.mcpServerId}
                    value={option.mcpServerId}
                    description={
                      option.attachedEndpointSlug
                        ? `/mcp/${option.attachedEndpointSlug}`
                        : "Not attached to this domain yet"
                    }
                  >
                    {rootServerLabel(option)}
                  </SelectItem>
                ))}
              </SelectGroup>
            ))}
          </SelectContent>
        </Select>
        {currentServer?.attachedEndpointSlug ? (
          <div className="text-muted-foreground flex flex-wrap items-center gap-2 text-sm">
            <code>{`https://${domain.domain}/`}</code>
            <span aria-hidden="true">→</span>
            <code>{`/mcp/${currentServer.attachedEndpointSlug}`}</code>
          </div>
        ) : (
          <Text variant="body" className="text-muted-foreground text-sm">
            Requests to the custom-domain root are not mapped.
          </Text>
        )}
        {!canManage && (
          <Text variant="body" className="text-muted-foreground text-sm">
            Organization admin permission is required to change this mapping.
          </Text>
        )}
      </div>
    );
  }

  return (
    <div className="border-border mt-4 border-t pt-4">
      <Text variant="body" className="font-medium">
        Default MCP server
      </Text>
      <Text
        variant="body"
        className="text-muted-foreground mt-1 mb-3 max-w-[65ch] text-sm"
      >
        Route requests to the custom-domain root to one MCP server.
      </Text>
      {content}
    </div>
  );
}

function ChatGPTAppVerificationControl({
  domain,
  canManage,
}: {
  domain: CustomDomain;
  canManage: boolean;
}) {
  const queryClient = useQueryClient();
  const savedToken = domain.openaiAppsChallengeToken ?? "";
  const [token, setToken] = useState(savedToken);
  const [error, setError] = useState("");
  const verificationURL = `https://${domain.domain}/.well-known/openai-apps-challenge`;

  useEffect(() => {
    setToken(savedToken);
  }, [savedToken]);

  const updateTokenMutation = useUpdateDomainMutation({
    onSuccess: async (updatedDomain) => {
      const updatedToken = updatedDomain.openaiAppsChallengeToken ?? "";
      setToken(updatedToken);
      setError("");
      await Promise.all([
        invalidateAllGetDomain(queryClient),
        invalidateAllListDomains(queryClient),
      ]);
      toast.success(
        updatedToken
          ? "ChatGPT app verification token saved"
          : "ChatGPT app verification token cleared",
      );
    },
    onError: (mutationError) => {
      setError(
        mutationError.message || "Failed to save the verification token",
      );
    },
  });

  function updateToken(nextToken: string) {
    updateTokenMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        updateDomainRequestBody: {
          openaiAppsChallengeToken: nextToken,
        },
      },
    });
  }

  const hasChanges = token !== savedToken;

  return (
    <div className="border-border mt-4 border-t pt-4">
      <Text variant="body" className="font-medium">
        ChatGPT app verification
      </Text>
      <Text
        variant="body"
        className="text-muted-foreground mt-1 max-w-[65ch] text-sm"
      >
        OpenAI&apos;s app-submission flow fetches this token to verify ownership
        of your custom domain.
      </Text>
      <div className="mt-3 max-w-xl space-y-3">
        <div className="bg-muted flex items-center gap-2 px-3 py-2">
          <code className="min-w-0 flex-1 text-xs break-all">
            {verificationURL}
          </code>
          <CopyButton
            text={verificationURL}
            size="xs"
            tooltip="Copy verification URL"
            className="shrink-0"
          />
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <div className="min-w-0 flex-1">
            <Input
              aria-label="ChatGPT app verification token"
              placeholder="Paste verification token"
              value={token}
              onChange={(value) => {
                setToken(value);
                setError("");
              }}
              onEnter={() => {
                if (canManage && hasChanges && !updateTokenMutation.isPending) {
                  updateToken(token);
                }
              }}
              maxLength={256}
              disabled={!canManage || updateTokenMutation.isPending}
              className="font-mono"
            />
          </div>
          <div className="flex shrink-0 gap-2">
            <Button
              size="sm"
              onClick={() => updateToken(token)}
              disabled={
                !canManage || !hasChanges || updateTokenMutation.isPending
              }
            >
              {updateTokenMutation.isPending ? "Saving..." : "Save"}
            </Button>
            {savedToken && (
              <Button
                variant="secondary"
                size="sm"
                onClick={() => updateToken("")}
                disabled={!canManage || updateTokenMutation.isPending}
              >
                Clear
              </Button>
            )}
          </div>
        </div>
        {error && (
          <Text variant="body" className="text-destructive text-sm">
            {error}
          </Text>
        )}
        {domain.ipAllowlist.length > 0 && (
          <div className="text-muted-foreground flex items-start gap-2 text-sm">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <span>
              Your IP allowlist also applies to this URL and may block
              OpenAI&apos;s verifier.
            </span>
          </div>
        )}
        {!canManage && (
          <Text variant="body" className="text-muted-foreground text-sm">
            Organization admin permission is required to change this token.
          </Text>
        )}
      </div>
    </div>
  );
}

function OrgDomainsInner() {
  const organization = useOrganization();
  const productTier = useProductTier();
  const { hasScope } = useRBAC();
  const canManageDomains = hasScope("org:admin");
  const queryClient = useQueryClient();
  const [isAddDomainDialogOpen, setIsAddDomainDialogOpen] = useState(false);
  const [copiedRecordValue, setCopiedRecordValue] = useState<string | null>(
    null,
  );
  const copyRecordResetTimer = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );
  const [isTxtCopied, setIsTxtCopied] = useState(false);
  const copyTxtResetTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [isCustomDomainModalOpen, setIsCustomDomainUpgradeModalOpen] =
    useState(false);
  const [isDeleteDomainDialogOpen, setIsDeleteDomainDialogOpen] =
    useState(false);
  const [domainInput, setDomainInput] = useState("");
  const [domainError, setDomainError] = useState("");
  // null = follow the suggestion; the user can override because apex
  // detection is a heuristic (delegated subzones can take a CNAME).
  const [recordTypeOverride, setRecordTypeOverride] = useState<
    "cname" | "a" | null
  >(null);
  const [pendingRootServerId, setPendingRootServerId] = useState<string | null>(
    null,
  );

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
    dnsConfig,
    isLoading: domainIsLoading,
    refetch: domainRefetch,
  } = useCustomDomain();

  const aRecords = dnsConfig?.aRecords ?? [];
  const CNAME_VALUE = dnsConfig?.cnameTarget || getCustomDomainCNAME();

  // Pre-registration the server has no domain to judge, so fall back to a
  // label-count heuristic; after registration the server's publicsuffix-based
  // suggestion wins. Both are defaults for a user-controllable toggle.
  const suggestedRecordType: "cname" | "a" =
    aRecords.length === 0
      ? "cname"
      : domain?.suggestedRecordType === "a" ||
          (!domain && validDomain && subdomain.split(".").length === 2)
        ? "a"
        : "cname";
  const recordType = recordTypeOverride ?? suggestedRecordType;

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

  const handleCopyRecordValue = async (value: string) => {
    if (!navigator.clipboard) return;
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      return;
    }
    setCopiedRecordValue(value);
    // A copy within the confirmation window supersedes the pending reset.
    if (copyRecordResetTimer.current)
      clearTimeout(copyRecordResetTimer.current);
    copyRecordResetTimer.current = setTimeout(
      () => setCopiedRecordValue(null),
      2000,
    );
  };
  const handleCopyTxt = async () => {
    if (!navigator.clipboard) return;
    try {
      await navigator.clipboard.writeText(txtValue);
    } catch {
      return;
    }
    setIsTxtCopied(true);
    if (copyTxtResetTimer.current) clearTimeout(copyTxtResetTimer.current);
    copyTxtResetTimer.current = setTimeout(() => setIsTxtCopied(false), 2000);
  };

  const rootMutation = useRootMcpEndpointMutation();
  const rootServersQuery = useRootMcpServers(undefined, undefined, {
    refetchOnWindowFocus: false,
  });
  const rootServerOptions = useMemo(
    () => rootServersQuery.data?.mcpServers ?? [],
    [rootServersQuery.data],
  );
  const rootServerGroups = useMemo(
    () => groupRootServerOptions(rootServerOptions),
    [rootServerOptions],
  );

  const registerDomainMutation = useRegisterDomainMutation({
    onSuccess: (created) => {
      setIsAddDomainDialogOpen(false);
      setDomainInput("");
      setDomainError("");
      setPendingIPs([]);
      setPendingIPsValid(true);
      setIsAllowlistExpanded(false);
      if (pendingRootServerId) {
        rootMutation.setRootMcpServer(created.id, pendingRootServerId);
        setPendingRootServerId(null);
      }
      setTimeout(() => {
        void domainRefetch();
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
      await invalidateAllListDomains(queryClient);
    },
  });

  const updateDomainMutation = useUpdateDomainMutation({
    onSuccess: async () => {
      setIsEditAllowlistOpen(false);
      await invalidateAllListDomains(queryClient);
    },
    onError: (error) => {
      setUpdateAllowlistError(error.message || "Failed to save allowlist");
    },
  });

  const checkDomainHealthMutation = useCheckDomainHealthMutation({
    onSuccess: async () => {
      await invalidateAllListDomains(queryClient);
      toast.success("Custom domain health check completed");
    },
    onError: (error) => {
      toast.error(error.message || "Failed to check custom domain health");
    },
  });

  // The same domain-wide list backs both root selection and the delete impact
  // preview, avoiding duplicate queries for identical organization state.
  const domainEndpointsQuery = useCustomDomainMcpEndpoints(
    undefined,
    undefined,
    {
      enabled: Boolean(domain?.domain),
    },
  );
  const impactedEndpoints = domainEndpointsQuery.data?.mcpEndpoints ?? [];

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
      void domainRefetch();
    }, 30000);
    return () => clearInterval(interval);
  }, [domain?.isUpdating, domainRefetch]);

  return (
    <SettingsPage
      title="Custom Domain"
      description="Connect a custom domain to serve your MCP servers from your own branded URL instead of the default platform domain."
    >
      {domain?.domain ? (
        <div className="border-border bg-card border p-4">
          <Stack direction="horizontal" justify="space-between" align="start">
            <Stack gap={1}>
              <Stack direction="horizontal" align="center" gap={2}>
                <Globe className="text-muted-foreground h-4 w-4" />
                <Text variant="body" className="font-mono font-medium">
                  {domain.domain}
                </Text>
                {domain.isUpdating ? (
                  <SimpleTooltip tooltip="Waiting for your DNS records to propagate and verify. This can take several hours with some providers; we re-check every few minutes (at most every 5 minutes apart) for up to 24 hours.">
                    <Loader2 className="h-4 w-4 animate-spin text-blue-500" />
                  </SimpleTooltip>
                ) : showCustomDomainAutoDisabled(domain) ? (
                  <SimpleTooltip tooltip="This domain was disabled after failing health checks for over a week">
                    <X className="h-4 w-4 stroke-3 text-red-500" />
                  </SimpleTooltip>
                ) : showCustomDomainUnhealthy(domain) ? (
                  <SimpleTooltip tooltip="The latest health check found a problem">
                    <AlertTriangle className="h-4 w-4 text-amber-500" />
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
              <Text
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Linked <HumanizeDateTime date={domain.createdAt} />
              </Text>
              <div className="mt-1 ml-6 flex flex-wrap items-center gap-2">
                <Text variant="body" className="text-muted-foreground text-sm">
                  Allowed IPs:
                </Text>
                {domain.ipAllowlist.length === 0 ? (
                  <Text
                    variant="body"
                    className="text-muted-foreground text-sm italic"
                  >
                    All (no restriction)
                  </Text>
                ) : (
                  domain.ipAllowlist.map((ip) => (
                    <Badge key={ip} variant="neutral" className="font-mono">
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
                    disabled={registerDomainMutation.isPending}
                    onClick={() => {
                      // While verification is pending, re-registering wakes
                      // the polling workflow for an immediate DNS re-check —
                      // no need to route through the setup dialog. Outside
                      // that state (auto-disabled, timed out) the dialog is
                      // the right entry point: it shows the records to fix.
                      if (domain.isUpdating) {
                        registerDomainMutation.mutate(
                          {
                            security: { sessionHeaderGramSession: "" },
                            request: {
                              createDomainRequestBody: {
                                domain: domain.domain,
                              },
                            },
                          },
                          {
                            onSuccess: () => {
                              toast.success("Checking DNS records now");
                            },
                          },
                        );
                      } else {
                        setIsAddDomainDialogOpen(true);
                      }
                    }}
                  >
                    {domain.isUpdating ? "Check now" : "Reverify"}
                  </Button>
                )}
                <Button
                  aria-label="Delete custom domain"
                  variant="tertiary"
                  size="sm"
                  onClick={() => setIsDeleteDomainDialogOpen(true)}
                  className="hover:text-destructive"
                  disabled={deleteDomainMutation.isPending}
                >
                  <Button.Icon>
                    <Trash2 className="h-4 w-4" />
                  </Button.Icon>
                </Button>
              </Stack>
            </RequireScope>
          </Stack>
          {showCustomDomainAutoDisabled(domain) && (
            <Alert variant="error" dismissible={false} className="mt-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="space-y-1">
                  <Text variant="body" className="font-medium">
                    This custom domain was disabled
                  </Text>
                  <Text variant="body" className="text-sm">
                    It failed health checks continuously for over a week, so its
                    routing and TLS certificate were removed.{" "}
                    <CustomDomainHealthMessage
                      issue={domain.healthIssue}
                      domainName={domain.domain}
                      recordType={domain.suggestedRecordType}
                      aRecords={aRecords}
                      cnameTarget={CNAME_VALUE}
                    />
                  </Text>
                  {domain.unhealthySince && (
                    <Text variant="body" className="text-sm opacity-80">
                      Unhealthy since{" "}
                      <HumanizeDateTime date={domain.unhealthySince} />
                    </Text>
                  )}
                  <Text variant="body" className="text-sm">
                    Fix the issue above, then reverify the domain to provision
                    it again.
                  </Text>
                </div>
                <RequireScope scope="org:admin" level="component">
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => setIsAddDomainDialogOpen(true)}
                  >
                    Reverify domain
                  </Button>
                </RequireScope>
              </div>
            </Alert>
          )}
          {showCustomDomainUnhealthy(domain) && (
            <Alert variant="warning" dismissible={false} className="mt-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="space-y-1">
                  <Text variant="body" className="font-medium">
                    This custom domain may not be working
                  </Text>
                  <Text variant="body" className="text-sm">
                    <CustomDomainHealthMessage
                      issue={domain.healthIssue}
                      domainName={domain.domain}
                      recordType={domain.suggestedRecordType}
                      aRecords={aRecords}
                      cnameTarget={CNAME_VALUE}
                    />
                  </Text>
                  {domain.healthCheckedAt && (
                    <Text variant="body" className="text-sm opacity-80">
                      Last checked{" "}
                      <HumanizeDateTime date={domain.healthCheckedAt} />
                    </Text>
                  )}
                </div>
                <RequireScope scope="org:admin" level="component">
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={checkDomainHealthMutation.isPending}
                    onClick={() =>
                      checkDomainHealthMutation.mutate({
                        security: { sessionHeaderGramSession: "" },
                      })
                    }
                  >
                    {checkDomainHealthMutation.isPending
                      ? "Checking..."
                      : "Check again"}
                  </Button>
                </RequireScope>
              </div>
            </Alert>
          )}
          <DefaultMcpServerControl
            domain={domain}
            canManage={canManageDomains}
          />
          <ChatGPTAppVerificationControl
            domain={domain}
            canManage={canManageDomains}
          />
        </div>
      ) : (
        !domainIsLoading && (
          <div className="border-border border border-dashed p-6">
            <Stack gap={2} align="center" justify="center">
              <Text variant="body" className="text-muted-foreground">
                No custom domain configured
              </Text>
              <Text variant="body" className="text-muted-foreground text-sm">
                You can connect one custom domain per organization for your MCP
                servers.
              </Text>
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
            <Text variant="body">
              Are you sure you want to remove{" "}
              <span className="font-bold italic">{domain?.domain}</span>? This
              will delete the associated ingress and TLS certificate.
            </Text>
            {domainEndpointsQuery.isLoading ? (
              <Text variant="small" muted>
                Checking for MCP endpoints under this domain&hellip;
              </Text>
            ) : impactedEndpoints.length > 0 ? (
              <div className="space-y-2">
                <Text variant="body" className="font-semibold">
                  {impactedEndpoints.length === 1
                    ? "1 MCP endpoint will be deactivated:"
                    : `${impactedEndpoints.length} MCP endpoints will be deactivated:`}
                </Text>
                <ul className="border-border max-h-48 list-disc space-y-1 overflow-y-auto border px-6 py-2">
                  {impactedEndpoints.map((endpoint) => (
                    <li key={endpoint.id}>
                      <Text variant="small">
                        <span className="font-mono">
                          {domain?.domain
                            ? customDomainMcpEndpointUrl(
                                domain.domain,
                                endpoint.slug,
                              )
                            : endpoint.slug}
                        </span>{" "}
                        <Text variant="small" as="span" muted>
                          &middot; {endpoint.projectName} &middot;{" "}
                          {mcpServerLabel(endpoint)}
                        </Text>
                      </Text>
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
            setPendingRootServerId(null);
            setRecordTypeOverride(null);
          }
        }}
      >
        <Dialog.Content className="max-w-lg">
          <Dialog.Header>
            <Dialog.Title>Connect a Custom Domain</Dialog.Title>
          </Dialog.Header>
          <div className="min-h-[420px] space-y-6 py-4">
            <div>
              <Text
                variant="body"
                className="mb-2 block text-lg font-extrabold"
              >
                Step 1
              </Text>
              <Text variant="body" className="text-muted-foreground mb-2">
                Enter your custom domain:
              </Text>
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
                  <Text variant="body" className="text-sm text-red-500">
                    {domainError}
                  </Text>
                )}
              </div>
            </div>
            <div>
              <Text
                variant="body"
                className="mb-2 block text-lg font-extrabold"
              >
                Step 2
              </Text>
              {aRecords.length > 0 && (
                <div className="mb-2 flex gap-1">
                  <Button
                    variant={recordType === "cname" ? "secondary" : "tertiary"}
                    size="sm"
                    onClick={() => setRecordTypeOverride("cname")}
                  >
                    CNAME (subdomain)
                  </Button>
                  <Button
                    variant={recordType === "a" ? "secondary" : "tertiary"}
                    size="sm"
                    onClick={() => setRecordTypeOverride("a")}
                  >
                    A record (apex domain)
                  </Button>
                </div>
              )}
              {recordType === "a" ? (
                <>
                  <Text variant="body" className="text-muted-foreground mb-2">
                    Create an A record for{" "}
                    <span className="font-mono break-all">{subdomain}</span>{" "}
                    (the zone root, often written as{" "}
                    <span className="font-mono">@</span>) pointing to:
                  </Text>
                  {aRecords.map((ip) => (
                    <div
                      key={ip}
                      className="bg-muted mt-2 flex items-center space-x-2 p-3"
                    >
                      <code className="flex-1 break-all">{ip}</code>
                      <Button
                        aria-label={
                          copiedRecordValue === ip
                            ? "A record value copied"
                            : "Copy A record value"
                        }
                        variant="tertiary"
                        size="sm"
                        onClick={() => void handleCopyRecordValue(ip)}
                        className="shrink-0"
                      >
                        <Button.Icon>
                          {copiedRecordValue === ip ? (
                            <CheckCircle2 className="h-4 w-4 text-green-500" />
                          ) : (
                            <Copy className="h-4 w-4" />
                          )}
                        </Button.Icon>
                      </Button>
                    </div>
                  ))}
                </>
              ) : (
                <>
                  <Text variant="body" className="text-muted-foreground mb-2">
                    Create a CNAME record for{" "}
                    <span className="font-mono break-all">{subdomain}</span>{" "}
                    pointing to the following:
                  </Text>
                  <div className="bg-muted mt-2 flex items-center space-x-2 p-3">
                    <code className="flex-1 break-all">{CNAME_VALUE}</code>
                    <Button
                      aria-label={
                        copiedRecordValue === CNAME_VALUE
                          ? "CNAME value copied"
                          : "Copy CNAME value"
                      }
                      variant="tertiary"
                      size="sm"
                      onClick={() => void handleCopyRecordValue(CNAME_VALUE)}
                      className="shrink-0"
                    >
                      <Button.Icon>
                        {copiedRecordValue === CNAME_VALUE ? (
                          <CheckCircle2 className="h-4 w-4 text-green-500" />
                        ) : (
                          <Copy className="h-4 w-4" />
                        )}
                      </Button.Icon>
                    </Button>
                  </div>
                </>
              )}
              <Text
                variant="body"
                className="text-muted-foreground mt-2 text-sm"
              >
                DNS changes can take a while to propagate — with some providers
                several hours. We re-check every few minutes (at most every 5
                minutes apart) for up to 24 hours, and you can trigger a check
                any time with “Check now”.
              </Text>
            </div>
            <div>
              <Text
                variant="body"
                className="mb-2 block text-lg font-extrabold"
              >
                Step 3
              </Text>
              <Text variant="body" className="text-muted-foreground mb-2">
                Create a TXT record at{" "}
                <span className="font-mono break-all">{txtName}</span> with the
                following value:
              </Text>
              <div className="bg-muted mt-2 flex items-center space-x-2 p-3">
                <code className="flex-1 break-all">{txtValue}</code>
                <Button
                  aria-label={
                    isTxtCopied ? "TXT value copied" : "Copy TXT value"
                  }
                  variant="tertiary"
                  size="sm"
                  onClick={() => void handleCopyTxt()}
                  className="shrink-0"
                >
                  <Button.Icon>
                    {isTxtCopied ? (
                      <CheckCircle2 className="h-4 w-4 text-green-500" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                  </Button.Icon>
                </Button>
              </div>
            </div>
            <div>
              <Text
                variant="body"
                className="mb-2 block text-lg font-extrabold"
              >
                CAA records
              </Text>
              <Text variant="body" className="text-muted-foreground mb-2">
                If this domain already has CAA records — common when migrating
                from Google or another certificate issuer — add a CAA record
                that allows Let's Encrypt. Skip this if you have no CAA records.
              </Text>
              <div className="bg-muted mt-2 flex items-center space-x-2 p-3">
                <code className="flex-1 break-all">
                  {customDomainCAARecord(subdomain)}
                </code>
                <Button
                  aria-label={
                    copiedRecordValue === customDomainCAARecord(subdomain)
                      ? "CAA value copied"
                      : "Copy CAA value"
                  }
                  variant="tertiary"
                  size="sm"
                  onClick={() =>
                    void handleCopyRecordValue(customDomainCAARecord(subdomain))
                  }
                  className="shrink-0"
                >
                  <Button.Icon>
                    {copiedRecordValue === customDomainCAARecord(subdomain) ? (
                      <CheckCircle2 className="h-4 w-4 text-green-500" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                  </Button.Icon>
                </Button>
              </div>
            </div>
            {!domain?.domain && rootServerOptions.length > 0 && (
              <div>
                <Text
                  variant="body"
                  className="mb-2 block text-lg font-extrabold"
                >
                  Step 4 (optional)
                </Text>
                <Text variant="body" className="text-muted-foreground mb-2">
                  Serve an MCP server at the domain root. It's applied the
                  moment the domain registers, so traffic routes as soon as DNS
                  cuts over — no follow-up step.
                </Text>
                <Select
                  value={pendingRootServerId ?? NO_ROOT_MCP_ENDPOINT}
                  onValueChange={(value) =>
                    setPendingRootServerId(
                      value === NO_ROOT_MCP_ENDPOINT ? null : value,
                    )
                  }
                >
                  <SelectTrigger className="w-full">
                    <SelectValue>
                      {pendingRootServerId
                        ? (rootServerOptions.find(
                            (option) =>
                              option.mcpServerId === pendingRootServerId,
                          )?.name ?? pendingRootServerId)
                        : "No root mapping"}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem
                      value={NO_ROOT_MCP_ENDPOINT}
                      description="Do not route the custom-domain root to an MCP server."
                    >
                      No root mapping
                    </SelectItem>
                    {rootServerGroups.map((group) => (
                      <SelectGroup key={group.projectId}>
                        <SelectLabel>{group.projectName}</SelectLabel>
                        {group.servers.map((option) => (
                          <SelectItem
                            key={option.mcpServerId}
                            value={option.mcpServerId}
                          >
                            {rootServerLabel(option)}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
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
                  <Text
                    variant="body"
                    className="text-muted-foreground mb-3 text-sm"
                  >
                    Restrict access to specific IP addresses or CIDR ranges.
                    Leave empty to allow all traffic.
                  </Text>
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
              <Text variant="body" className="text-destructive text-sm">
                {updateAllowlistError}
              </Text>
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
    </SettingsPage>
  );
}
