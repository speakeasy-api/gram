import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useProductTier } from "@/hooks/useProductTier";
import { useCustomDomain } from "@/hooks/useToolsetUrl";
import { HumanizeDateTime } from "@/lib/dates";
import { cn, getCustomDomainCNAME } from "@/lib/utils";
import { useDeleteDomainMutation } from "@gram/client/react-query/deleteDomain";
import { invalidateAllGetDomain } from "@gram/client/react-query/getDomain";
import { useRegisterDomainMutation } from "@gram/client/react-query/registerDomain";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  Check,
  CheckCircle2,
  Copy,
  Globe,
  Loader2,
  Trash2,
  X,
} from "lucide-react";
import { useEffect, useState } from "react";
import { RequireScope } from "@/components/require-scope";

export default function OrgDomains() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Title>Custom Domain</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <OrgDomainsInner />
        </RequireScope>
      </Page.Body>
    </Page>
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
        URL instead of the default Gram domain.
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
            </Stack>
            <RequireScope scope="org:admin" level="section">
              <Stack direction="horizontal" gap={2}>
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
        onOpenChange={setIsAddDomainDialogOpen}
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
            <div className="mt-4 flex justify-end">
              <RequireScope scope="org:admin" level="component">
                <Button
                  onClick={handleRegisterDomain}
                  disabled={
                    !domainInput.trim() ||
                    !!domainError ||
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
