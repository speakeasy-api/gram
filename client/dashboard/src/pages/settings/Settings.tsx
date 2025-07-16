import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization, useSession } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { Key } from "@gram/client/models/components";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import {
  invalidateListAPIKeys,
  useListAPIKeysSuspense,
} from "@gram/client/react-query/listAPIKeys";
import { useRegisterDomainMutation } from "@gram/client/react-query/registerDomain";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";
import { Column, Stack, Table } from "@speakeasy-api/moonshine";
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
import { useCustomDomain } from "../mcp/MCPDetails";

export default function Settings() {
  const organization = useOrganization();
  const session = useSession();
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [newKeyName, setNewKeyName] = useState("");
  const [keyToRevoke, setKeyToRevoke] = useState<Key | null>(null);
  const [newlyCreatedKey, setNewlyCreatedKey] = useState<Key | null>(null);
  const [isCopied, setIsCopied] = useState(false);
  const queryClient = useQueryClient();
  const [isAddDomainDialogOpen, setIsAddDomainDialogOpen] = useState(false);
  const [isCnameCopied, setIsCnameCopied] = useState(false);
  const [isTxtCopied, setIsTxtCopied] = useState(false);
  const [isCustomDomainModalOpen, setIsCustomDomainModalOpen] = useState(false);
  const [domainInput, setDomainInput] = useState("");
  const [domainError, setDomainError] = useState("");
  const CNAME_VALUE = "cname.getgram.ai.";

  // Dynamic values based on domain input
  const subdomain = domainInput.trim() || "sub.yourdomain.com";
  const txtName = `_gram.${subdomain}`;
  const txtValue = `gram-domain-verify=${subdomain},${organization.id}`;

  const { data: keysData } = useListAPIKeysSuspense();
  const {
    domain,
    isLoading: domainIsLoading,
    refetch: domainRefetch,
  } = useCustomDomain();

  // Initialize domain input with existing domain if available
  useEffect(() => {
    if (domain?.domain && !domainInput) {
      setDomainInput(domain.domain);
    }
  }, [domain?.domain, domainInput]);

  // Domain validation regex (same as used in the backend)
  const domainRegex = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z]{2,})+$/i;

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

  const createKeyMutation = useCreateAPIKeyMutation({
    onSuccess: async (data) => {
      setNewKeyName("");
      setNewlyCreatedKey(data);
      await invalidateListAPIKeys(queryClient, [{ gramSession: "" }]);
      await queryClient.refetchQueries({
        queryKey: ["@gram/client", "keys", "list"],
      });
    },
  });

  const revokeKeyMutation = useRevokeAPIKeyMutation({
    onSuccess: async () => {
      setKeyToRevoke(null);
      await invalidateListAPIKeys(queryClient, [{ gramSession: "" }]);
      await queryClient.refetchQueries({
        queryKey: ["@gram/client", "keys", "list"],
      });
    },
  });

  const registerDomainMutation = useRegisterDomainMutation({
    onSuccess: () => {
      setIsAddDomainDialogOpen(false);
      setDomainInput("");
      setDomainError("");
      // Wait 2 seconds before refetching domain data
      setTimeout(() => {
        domainRefetch();
      }, 2000);
    },
    onError: (error) => {
      setDomainError(error.message || "Failed to register domain");
    },
  });

  const handleCreateKey = () => {
    createKeyMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        createKeyForm: {
          name: newKeyName,
        },
      },
    });
  };

  const handleRevokeKey = () => {
    if (!keyToRevoke) return;

    revokeKeyMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        id: keyToRevoke.id,
      },
    });
  };

  const handleCopyToken = async () => {
    if (newlyCreatedKey?.key) {
      await navigator.clipboard.writeText(newlyCreatedKey.key);
      setIsCopied(true);
      setTimeout(() => setIsCopied(false), 2000);
    }
  };

  const handleCloseCreateDialog = () => {
    setIsCreateDialogOpen(false);
    setNewlyCreatedKey(null);
    setIsCopied(false);
  };

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

  const apiKeyColumns: Column<Key>[] = [
    {
      key: "name",
      header: "Name",
      width: "1fr",
      render: (key: Key) => <Type variant="body">{key.name}</Type>,
    },
    {
      key: "key",
      header: "Key",
      width: "1fr",
      render: (key: Key) => <Type variant="body">{key.keyPrefix}</Type>,
    },
    {
      key: "scopes",
      header: "Scopes",
      width: "1fr",
      render: (key: Key) => <Type variant="body">{key.scopes.join(",")}</Type>,
    },
    {
      key: "createdAt",
      header: "Created At",
      width: "1fr",
      render: (key: Key) => <HumanizeDateTime date={key.createdAt} />,
    },
    {
      key: "actions",
      header: "",
      width: "80px",
      render: (key: Key) => (
        <Button
          variant="ghost"
          size="icon"
          onClick={() => setKeyToRevoke(key)}
          className="hover:text-destructive"
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      ),
    },
  ];

  // refetch as domain is being verified
  useEffect(() => {
    if (!domain?.isUpdating) return;
    const interval = setInterval(() => {
      domainRefetch();
    }, 30000);
    return () => clearInterval(interval);
  }, [domain?.isUpdating, domainRefetch]);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Stack direction="horizontal" justify="space-between">
          <Heading variant="h4">API Keys</Heading>
          <Button onClick={() => setIsCreateDialogOpen(true)}>
            Create API Key
          </Button>
        </Stack>
        <Table
          columns={apiKeyColumns}
          data={keysData?.keys ?? []}
          rowKey={(row) => row.id}
          noResultsMessage={
            <Stack
              direction="horizontal"
              gap={2}
              className="h-full p-4 bg-background"
              align="center"
              justify="center"
            >
              <Type variant="body">No API keys yet.</Type>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setIsCreateDialogOpen(true)}
                icon={"key-round"}
              >
                Create
              </Button>
            </Stack>
          }
        />

        <Dialog
          open={isCreateDialogOpen}
          onOpenChange={handleCloseCreateDialog}
        >
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>
                {newlyCreatedKey ? "API Key Created" : "Create New API Key"}
              </Dialog.Title>
            </Dialog.Header>
            {newlyCreatedKey ? (
              <div className="space-y-4 py-4">
                <div className="rounded-lg border-yellow-500/50 bg-yellow-50/50 text-yellow-600 p-4 text-sm">
                  You won't be able to see this token value again once you close
                  this dialog.
                </div>
                <div className="flex items-center space-x-2 bg-muted p-3 rounded-md">
                  <code className="flex-1 break-all">
                    {newlyCreatedKey.key}
                  </code>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={handleCopyToken}
                    className="shrink-0"
                  >
                    {isCopied ? (
                      <CheckCircle2 className="h-4 w-4 text-green-500" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                  </Button>
                </div>
                <div className="flex justify-end">
                  <Button onClick={handleCloseCreateDialog}>Close</Button>
                </div>
              </div>
            ) : (
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Input
                    placeholder="Enter key name"
                    value={newKeyName}
                    onChange={setNewKeyName}
                  />
                </div>
                <div className="flex justify-end space-x-2">
                  <Button variant="secondary" onClick={handleCloseCreateDialog}>
                    Cancel
                  </Button>
                  <Button
                    onClick={handleCreateKey}
                    disabled={!newKeyName || createKeyMutation.isPending}
                  >
                    Create
                  </Button>
                </div>
              </div>
            )}
          </Dialog.Content>
        </Dialog>

        <Dialog
          open={!!keyToRevoke}
          onOpenChange={(open) => !open && setKeyToRevoke(null)}
        >
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Revoke API Key</Dialog.Title>
            </Dialog.Header>
            <div className="space-y-4 py-4">
              <Type variant="body">
                Are you sure you want to revoke the API key{" "}
                <span className="italic font-bold">{keyToRevoke?.name}</span>?
                This action cannot be undone.
              </Type>
              <div className="flex justify-end space-x-2">
                <Button
                  variant="secondary"
                  onClick={() => setKeyToRevoke(null)}
                >
                  Cancel
                </Button>
                <Button
                  variant="destructive"
                  onClick={handleRevokeKey}
                  disabled={revokeKeyMutation.isPending}
                >
                  Revoke Key
                </Button>
              </div>
            </div>
          </Dialog.Content>
        </Dialog>

        <Stack direction="horizontal" justify="space-between" className="mt-8">
          <Heading variant="h4">Custom Domains</Heading>
          {session.gramAccountType === "free" && (
            <Type className="text-muted-foreground">
              Contact gram support to get access to custom domains for your
              account.
            </Type>
          )}
          {!domainIsLoading && !domain?.verified && (
            <Button
              onClick={() => {
                if (session.gramAccountType === "free") {
                  setIsCustomDomainModalOpen(true);
                } else {
                  setIsAddDomainDialogOpen(true);
                }
              }}
              disabled={domain?.isUpdating}
            >
              {domain?.domain ? "Verify Domain" : "Add Domain"}
            </Button>
          )}
        </Stack>
        <Table
          noResultsMessage={
            <Stack
              direction="horizontal"
              gap={2}
              className="h-full p-4 bg-background"
              align="center"
              justify="center"
            >
              <Type variant="body">No custom domains yet.</Type>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setIsAddDomainDialogOpen(true)}
                icon={"globe"}
              >
                Add
              </Button>
            </Stack>
          }
          columns={[
            {
              key: "domain",
              header: "Domain",
              width: "1fr",
              render: (row) => <Type variant="body">{row.domain}</Type>,
            },
            {
              key: "createdAt",
              header: "Date Linked",
              width: "1fr",
              render: (row) => (
                <Type variant="body">
                  <HumanizeDateTime date={row.createdAt} />
                </Type>
              ),
            },
            {
              key: "verified",
              header: "Verified",
              width: "120px",
              render: (row) => (
                <span className="flex justify-center items-center">
                  {row.isUpdating ? (
                    <SimpleTooltip tooltip="Your domain is being verified. Please refresh the page in a minute or two.">
                      <Loader2 className="w-5 h-5 animate-spin text-blue-500" />
                    </SimpleTooltip>
                  ) : row.verified ? (
                    <Check
                      className={cn("w-5 h-5 stroke-3", "text-green-500")}
                    />
                  ) : (
                    <SimpleTooltip tooltip="Domain verification failed, please ensure your DNS records have been setup correctly">
                      <X className="w-5 h-5 stroke-3 text-red-500" />
                    </SimpleTooltip>
                  )}
                </span>
              ),
            },
          ]}
          data={domain?.domain ? [domain] : []}
          rowKey={(row) => row.id}
        />

        <Dialog
          open={isAddDomainDialogOpen}
          onOpenChange={setIsAddDomainDialogOpen}
        >
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Connect a Custom Domain</Dialog.Title>
            </Dialog.Header>
            <div className="space-y-6 py-4">
              <div>
                <Type
                  variant="body"
                  className="font-extrabold text-lg mb-2 block"
                >
                  Step 1
                </Type>
                <Type variant="body" className="text-muted-foreground mb-2">
                  Create a CNAME record for{" "}
                  <span className="font-mono">{subdomain}</span> pointing to the
                  following:
                </Type>
                <div className="flex items-center space-x-2 bg-muted p-3 rounded-md mt-2">
                  <code className="flex-1 break-all">{CNAME_VALUE}</code>
                  <Button
                    variant="ghost"
                    size="icon"
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
                  className="font-extrabold text-lg mb-2 block"
                >
                  Step 2
                </Type>
                <Type variant="body" className="text-muted-foreground mb-2">
                  Create a TXT record at{" "}
                  <span className="font-mono">{txtName}</span> with the
                  following value:
                </Type>
                <div className="flex items-center space-x-2 bg-muted p-3 rounded-md mt-2">
                  <code className="flex-1 break-all">{txtValue}</code>
                  <Button
                    variant="ghost"
                    size="icon"
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
                <Type
                  variant="body"
                  className="font-extrabold text-lg mb-2 block"
                >
                  Step 3
                </Type>
                <Type variant="body" className="text-muted-foreground mb-2">
                  Enter a custom domain:
                </Type>
                <div className="space-y-2">
                  <Input
                    placeholder="Enter your domain (chat.yourdomain.com)"
                    value={domainInput}
                    onChange={handleDomainInputChange}
                    className={cn(
                      domainError && "border-red-500",
                      domain?.domain &&
                        "bg-muted text-muted-foreground cursor-not-allowed"
                    )}
                    readOnly={!!domain?.domain}
                  />
                  {domainError && (
                    <Type variant="body" className="text-red-500 text-sm">
                      {domainError}
                    </Type>
                  )}
                </div>
                <div className="flex justify-end mt-4">
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
                      ? "Verify"
                      : "Register"}
                  </Button>
                </div>
              </div>
            </div>
          </Dialog.Content>
        </Dialog>
        <FeatureRequestModal
          isOpen={isCustomDomainModalOpen}
          onClose={() => setIsCustomDomainModalOpen(false)}
          title="Custom Domains"
          description="Custom domains require upgrading to a pro account type. Someone should be in touch shortly, or feel free to book a meeting directly."
          actionType="custom_domain"
          icon={Globe}
          accountUpgrade
        />
      </Page.Body>
    </Page>
  );
}
