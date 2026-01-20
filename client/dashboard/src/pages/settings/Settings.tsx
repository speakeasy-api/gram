import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization, useSession } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import { assert, cn, getCustomDomainCNAME } from "@/lib/utils";
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
import { Check, CheckCircle2, Copy, Globe, Loader2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useCustomDomain } from "@/hooks/useToolsetUrl";
import { SettingsProjectsTable } from "./SettingsProjectsTable";

export default function Settings() {
  const organization = useOrganization();
  const session = useSession();
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
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
  const CNAME_VALUE = getCustomDomainCNAME();

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

  const handleCreateKey: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    const formEl = e.currentTarget;
    const formData = new FormData(formEl);
    const newKeyName = formData.get("name");
    assert(typeof newKeyName === "string", "Key name must be a string");
    const scope = formData.get("scope");
    assert(typeof scope === "string", "Scope must be a string");

    createKeyMutation.mutate(
      {
        security: { sessionHeaderGramSession: "" },
        request: {
          createKeyForm: {
            name: newKeyName,
            scopes: [scope],
          },
        },
      },
      {
        onSuccess: () => {
          formEl.reset();
        },
      },
    );
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
      render: (key: Key) => <Type variant="body">{key.scopes.join(", ")}</Type>,
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
          variant="tertiary"
          size="sm"
          onClick={() => setKeyToRevoke(key)}
          className="hover:text-destructive"
        >
          <Button.Text>
            <Icon name="trash-2" className="h-4 w-4" />
          </Button.Text>
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
        <SettingsProjectsTable />

        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          className="mt-8"
        >
          <Heading variant="h4">API Keys</Heading>
          <Button onClick={() => setIsCreateDialogOpen(true)}>
            New API Key
          </Button>
        </Stack>
        <Table
          columns={apiKeyColumns}
          data={keysData?.keys ?? []}
          rowKey={(row) => row.id}
          className="min-h-fit max-h-[500px] overflow-y-auto"
          noResultsMessage={
            <Stack
              gap={2}
              className="h-full p-4 bg-background"
              align="center"
              justify="center"
            >
              <Type variant="body">No API keys yet</Type>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setIsCreateDialogOpen(true)}
              >
                <Button.LeftIcon>
                  <Icon name="key-round" className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Create Key</Button.Text>
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
                <div className="rounded-lg border border-yellow-500/50 bg-yellow-600/50 text-foreground p-4 text-sm">
                  You will not be able to see this token value again once you
                  close this dialog. Copy it now and store it securely.
                </div>
                <div className="flex items-center space-x-2 bg-muted p-3 rounded-md">
                  <code className="flex-1 break-all">
                    {newlyCreatedKey.key}
                  </code>
                  <Button
                    variant="tertiary"
                    size="sm"
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
              <form className="space-y-4 py-4" onSubmit={handleCreateKey}>
                <InputField
                  label="Key name"
                  name="name"
                  required
                  autoFocus
                  autoCapitalize="off"
                  autoComplete="off"
                  autoCorrect="off"
                />

                <AnyField
                  label="Scope"
                  optionality="hidden"
                  render={() => {
                    return (
                      <RadioGroup name="scope" defaultValue="consumer">
                        <div className="flex items-center gap-3">
                          <RadioGroupItem value="consumer" id="r1" />
                          <Label className="leading-normal" htmlFor="r1">
                            Consumer: can query/modify toolsets, read data and
                            access MCP servers.
                          </Label>
                        </div>
                        <div className="flex items-center gap-3">
                          <RadioGroupItem value="producer" id="r2" />
                          <Label className="leading-normal" htmlFor="r2">
                            Producer: can upload OpenAPI documents, trigger
                            deployments, query/modify toolsets, read data and
                            access MCP servers.
                          </Label>
                        </div>
                        <div className="flex items-center gap-3">
                          <RadioGroupItem value="chat" id="r3" />
                          <Label className="leading-normal" htmlFor="r3">
                            Chat: can use the chat API to interact with models.
                          </Label>
                        </div>
                      </RadioGroup>
                    );
                  }}
                />
                <div className="flex justify-end space-x-2">
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={handleCloseCreateDialog}
                  >
                    Cancel
                  </Button>
                  <Button type="submit" disabled={createKeyMutation.isPending}>
                    Create
                  </Button>
                </div>
              </form>
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
                  variant="destructive-primary"
                  onClick={handleRevokeKey}
                  disabled={revokeKeyMutation.isPending}
                >
                  Revoke Key
                </Button>
              </div>
            </div>
          </Dialog.Content>
        </Dialog>

        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          className="mt-8"
        >
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
          data={domain?.domain ? [domain] : []}
          rowKey={(row) => row.id}
          className="min-h-fit"
          noResultsMessage={
            <Stack
              gap={2}
              className="h-full p-4 bg-background"
              align="center"
              justify="center"
            >
              <Type variant="body">No custom domains yet</Type>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => {
                  if (session.gramAccountType === "free") {
                    setIsCustomDomainModalOpen(true);
                  } else {
                    setIsAddDomainDialogOpen(true);
                  }
                }}
                disabled={domain?.isUpdating}
              >
                <Button.LeftIcon>
                  <Icon name="globe" className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Add Domain</Button.Text>
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
                    <Type variant="body" className="text-red-500 text-sm">
                      {domainError}
                    </Type>
                  )}
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
                  Create a CNAME record for{" "}
                  <span className="font-mono">{subdomain}</span> pointing to the
                  following:
                </Type>
                <div className="flex items-center space-x-2 bg-muted p-3 rounded-md mt-2">
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
                  className="font-extrabold text-lg mb-2 block"
                >
                  Step 3
                </Type>
                <Type variant="body" className="text-muted-foreground mb-2">
                  Create a TXT record at{" "}
                  <span className="font-mono">{txtName}</span> with the
                  following value:
                </Type>
                <div className="flex items-center space-x-2 bg-muted p-3 rounded-md mt-2">
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
