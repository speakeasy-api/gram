import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { HumanizeDateTime } from "@/lib/dates";
import { Key } from "@gram/client/models/components";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import {
  invalidateListAPIKeys,
  useListAPIKeysSuspense,
} from "@gram/client/react-query/listAPIKeys";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";
import { useGetDomain } from "@gram/client/react-query";
import { useRegisterDomainMutation } from "@gram/client/react-query/registerDomain";
import { Column, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Copy, Trash2, Check, X, Loader2 } from "lucide-react";
import { useState, useEffect } from "react";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { SimpleTooltip } from "@/components/ui/tooltip";

export default function Settings() {
  const organization = useOrganization();
  const session = useSession();
  const telemetry = useTelemetry();
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [newKeyName, setNewKeyName] = useState("");
  const [keyToRevoke, setKeyToRevoke] = useState<Key | null>(null);
  const [newlyCreatedKey, setNewlyCreatedKey] = useState<Key | null>(null);
  const [isCopied, setIsCopied] = useState(false);
  const queryClient = useQueryClient();
  const [isAddDomainDialogOpen, setIsAddDomainDialogOpen] = useState(false);
  const [isCnameCopied, setIsCnameCopied] = useState(false);
  const [isTxtCopied, setIsTxtCopied] = useState(false);
  const [domainInput, setDomainInput] = useState("");
  const [domainError, setDomainError] = useState("");
  const CNAME_VALUE = "cname.getgram.ai.";
  const SUBDOMAIN = "sub.yourdomain.com";
  const TXT_NAME = `_gram.${SUBDOMAIN}`;
  const TXT_VALUE = `gram-domain-verify=${SUBDOMAIN},${organization.id}`;

  const { data: keysData } = useListAPIKeysSuspense();
  const domain = useGetDomain(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
  });

  // Initialize domain input with existing domain if available
  useEffect(() => {
    if (domain.data?.domain && !domainInput) {
      setDomainInput(domain.data.domain);
    }
  }, [domain.data?.domain, domainInput]);

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
    await navigator.clipboard.writeText(TXT_VALUE);
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
        domain.refetch();
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
    if (!domain.data?.isUpdating) return;
    const interval = setInterval(() => {
      domain.refetch();
    }, 30000);
    return () => clearInterval(interval);
  }, [domain.data?.isUpdating, domain.refetch]);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="flex justify-between items-center mb-4">
          <Heading variant="h4">API Keys</Heading>
          <Button onClick={() => setIsCreateDialogOpen(true)}>
            Create API Key
          </Button>
        </div>
        <Table
          columns={apiKeyColumns}
          data={keysData?.keys ?? []}
          rowKey={(row) => row.id}
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

        <div className="mt-10">
          <Heading variant="h4" className="mb-4">
            Custom Domains
          </Heading>
          {session.gramAccountType === "free" && (
            <Type className="text-muted-foreground mb-2">
              Contact gram support to get access to custom domains for your
              account.
            </Type>
          )}
          {!domain.isLoading && !domain.data?.verified && (
            <div className="flex justify-end mb-6">
              <Button
                onClick={() => {
                  if (session.gramAccountType === "free") {
                    telemetry.capture("feature_requested", {
                      action: "custom_domain",
                    });
                    alert(
                      "Custom domains for your account require approval by the Speakeasy team. Someone should be in touch shortly, or feel free to reach out directly."
                    );
                  } else {
                    setIsAddDomainDialogOpen(true);
                  }
                }}
                disabled={domain.data?.isUpdating}
              >
                {domain.data?.domain ? "Verify Domain" : "Add Domain"}
              </Button>
            </div>
          )}
          <Table
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
            data={domain.data?.domain ? [domain.data] : []}
            rowKey={(row) => row.id}
          />
        </div>

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
                  <span className="font-mono">{SUBDOMAIN}</span> pointing to the
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
                  <span className="font-mono">{TXT_NAME}</span> with the
                  following value:
                </Type>
                <div className="flex items-center space-x-2 bg-muted p-3 rounded-md mt-2">
                  <code className="flex-1 break-all">{TXT_VALUE}</code>
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
                      domain.data?.domain &&
                        "bg-muted text-muted-foreground cursor-not-allowed"
                    )}
                    readOnly={!!domain.data?.domain}
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
                      : domain.data?.domain
                      ? "Verify"
                      : "Register"}
                  </Button>
                </div>
              </div>
            </div>
          </Dialog.Content>
        </Dialog>
      </Page.Body>
    </Page>
  );
}
