import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import {
  useIsAdmin,
  useOrganization,
  useProject,
  useSession,
} from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useCustomDomain } from "@/hooks/useToolsetUrl";
import { HumanizeDateTime } from "@/lib/dates";
import { assert, cn, getCustomDomainCNAME } from "@/lib/utils";
import { Key } from "@gram/client/models/components";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import {
  invalidateListAPIKeys,
  useListAPIKeysSuspense,
} from "@gram/client/react-query/listAPIKeys";
import { useDeleteDomainMutation } from "@gram/client/react-query/deleteDomain";
import { invalidateAllGetDomain } from "@gram/client/react-query/getDomain";
import { useRegisterDomainMutation } from "@gram/client/react-query/registerDomain";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";
import { Button, Column, Icon, Stack, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  Check,
  CheckCircle2,
  Copy,
  Globe,
  Loader2,
  ShieldAlert,
  Trash2,
  X,
} from "lucide-react";
import { useEffect, useState } from "react";
import { SettingsProjectsTable } from "./SettingsProjectsTable";

export default function Settings() {
  const organization = useOrganization();
  const session = useSession();
  const isAdmin = useIsAdmin();
  const client = useSdkClient();
  const project = useProject();

  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [keyToRevoke, setKeyToRevoke] = useState<Key | null>(null);
  const [newlyCreatedKey, setNewlyCreatedKey] = useState<Key | null>(null);
  const [isCopied, setIsCopied] = useState(false);
  const queryClient = useQueryClient();
  const [isAddDomainDialogOpen, setIsAddDomainDialogOpen] = useState(false);
  const [isCnameCopied, setIsCnameCopied] = useState(false);
  const [isTxtCopied, setIsTxtCopied] = useState(false);
  const [isCustomDomainModalOpen, setIsCustomDomainModalOpen] = useState(false);
  const [isDeleteDomainDialogOpen, setIsDeleteDomainDialogOpen] =
    useState(false);
  const [domainInput, setDomainInput] = useState("");
  const [domainError, setDomainError] = useState("");
  const CNAME_VALUE = getCustomDomainCNAME();

  // Domain validation regex (same as used in the backend)
  const domainRegex = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z]{2,})+$/i;

  // Only show real values once a valid domain is entered
  const validDomain =
    domainInput.trim() && domainRegex.test(domainInput.trim());
  const subdomain = validDomain ? domainInput.trim() : "sub.yourdomain.com";
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

  const deleteDomainMutation = useDeleteDomainMutation({
    onSuccess: async () => {
      setIsDeleteDomainDialogOpen(false);
      setDomainInput("");
      await invalidateAllGetDomain(queryClient);
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
      key: "lastAccessedAt",
      header: "Last Accessed At",
      width: "1fr",
      render: (key: Key) =>
        key.lastAccessedAt ? (
          <HumanizeDateTime date={key.lastAccessedAt} />
        ) : (
          "-"
        ),
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
          <Button.LeftIcon>
            <Icon name="trash-2" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text className="sr-only">Revoke API key</Button.Text>
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

        <Heading variant="h4" className="mt-8">
          Custom Domain
        </Heading>
        {domain?.domain ? (
          <div className="rounded-lg border border-border bg-card p-4">
            <Stack direction="horizontal" justify="space-between" align="start">
              <Stack gap={1}>
                <Stack direction="horizontal" align="center" gap={2}>
                  <Globe className="h-4 w-4 text-muted-foreground" />
                  <Type variant="body" className="font-mono font-medium">
                    {domain.domain}
                  </Type>
                  {domain.isUpdating ? (
                    <SimpleTooltip tooltip="Your domain is being verified. This may take a few minutes.">
                      <Loader2 className="w-4 h-4 animate-spin text-blue-500" />
                    </SimpleTooltip>
                  ) : domain.verified ? (
                    <SimpleTooltip tooltip="Domain verified and active">
                      <Check className="w-4 h-4 stroke-3 text-green-500" />
                    </SimpleTooltip>
                  ) : (
                    <SimpleTooltip tooltip="Domain verification failed. Ensure your DNS records are set up correctly.">
                      <X className="w-4 h-4 stroke-3 text-red-500" />
                    </SimpleTooltip>
                  )}
                </Stack>
                <Type
                  variant="body"
                  className="text-muted-foreground text-sm ml-6"
                >
                  Linked <HumanizeDateTime date={domain.createdAt} />
                </Type>
              </Stack>
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
            </Stack>
          </div>
        ) : (
          !domainIsLoading && (
            <div className="rounded-lg border border-dashed border-border p-6">
              <Stack gap={2} align="center" justify="center">
                <Type variant="body" className="text-muted-foreground">
                  No custom domain configured
                </Type>
                <Type variant="body" className="text-muted-foreground text-sm">
                  You can connect one custom domain per organization for your
                  MCP servers.
                </Type>
                <Button
                  size="sm"
                  variant="secondary"
                  className="mt-2"
                  onClick={() => {
                    if (session.gramAccountType === "free") {
                      setIsCustomDomainModalOpen(true);
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
                <span className="italic font-bold">{domain?.domain}</span>? This
                will delete the associated ingress and TLS certificate.
              </Type>
              <div className="flex justify-end space-x-2">
                <Button
                  variant="secondary"
                  onClick={() => setIsDeleteDomainDialogOpen(false)}
                >
                  Cancel
                </Button>
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
            <div className="space-y-6 py-4 min-h-[420px]">
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
                  <span className="font-mono break-all">{subdomain}</span>{" "}
                  pointing to the following:
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
                  <span className="font-mono break-all">{txtName}</span> with
                  the following value:
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
                      ? "Reverify"
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

        {isAdmin && (
          <div className="mt-12 p-4 rounded-lg bg-red-500/5 border border-red-500/20">
            <Stack
              direction="horizontal"
              align="center"
              gap={2}
              className="mb-3"
            >
              <ShieldAlert className="w-5 h-5 text-red-500" />
              <Heading variant="h4" className="text-red-600 dark:text-red-400">
                Admin Only
              </Heading>
            </Stack>
            <dl className="grid grid-cols-[max-content_auto] gap-x-6 gap-y-2 mb-8">
              <dt className="text-end">Organization ID</dt>
              <dd className="font-mono">{organization.id}</dd>
              <dt className="text-end">Project ID</dt>
              <dd className="font-mono">{project.id}</dd>
            </dl>

            <Type variant="body" className="text-muted-foreground mb-4">
              Override to a different organization by entering its slug below.
            </Type>
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                const formData = new FormData(e.currentTarget);
                const val = formData.get("gram_admin_override");
                if (typeof val !== "string" || !val.trim()) {
                  return;
                }

                document.cookie = `gram_admin_override=${val.trim()}; path=/; max-age=31536000;`;
                await client.auth.logout();
                window.location.href = "/login";
              }}
              className="flex gap-2 max-w-md"
            >
              <Input
                placeholder="organization-slug"
                name="gram_admin_override"
                className="flex-1"
                required
              />
              <Button type="submit">Go to Org</Button>
              <Button
                variant="secondary"
                type="button"
                onClick={async () => {
                  document.cookie = `gram_admin_override=; path=/; max-age=0;`;
                  await client.auth.logout();
                  window.location.href = "/login";
                }}
              >
                Clear override
              </Button>
            </form>
          </div>
        )}
      </Page.Body>
    </Page>
  );
}
