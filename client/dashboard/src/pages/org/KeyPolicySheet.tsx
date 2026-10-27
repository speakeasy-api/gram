import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { clientTypeLabel } from "@/lib/auth-clients";
import type { APIKeyPolicyGrant } from "@gram/client/models/components/apikeypolicygrant.js";
import { APIKeyPolicyGrantScope } from "@gram/client/models/components/apikeypolicygrant.js";
import type { Key } from "@gram/client/models/components/key.js";
import { useAuthClientsInfinite } from "@gram/client/react-query/authClients.js";
import { invalidateAllListAPIKeys } from "@gram/client/react-query/listAPIKeys.js";
import { useUpdateAPIKeyPolicyMutation } from "@gram/client/react-query/updateAPIKeyPolicy.js";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useOrgMcpServers } from "../access/useOrgMcpServers";
import { projectBindingLabel } from "./api-key-project-binding";
import {
  ALLOW_SCOPE,
  BLOCK_SCOPE,
  grantEffect,
  grantKey,
  grantKind,
  type Effect,
} from "./key-policy";

/**
 * What an ordinary key may reach on MCP servers, and which upstream
 * credentials it may use there. A key holds only what is listed: it never
 * acts as the person who created it.
 */
export function KeyPolicySheet({
  apiKey,
  onOpenChange,
}: {
  apiKey: Key;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const organization = useOrganization();
  const queryClient = useQueryClient();
  const clients = useAuthClientsInfinite({ limit: 100 }, undefined, {
    throwOnError: false,
  });
  const {
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = clients;
  // Every credential is needed to name the grants and fill the picker. A
  // failed page stops the walk, or it would retry without end.
  const draining = !!hasNextPage && !isFetchNextPageError;
  useEffect(() => {
    if (draining && !isFetchingNextPage) void fetchNextPage();
  }, [draining, isFetchingNextPage, fetchNextPage]);
  const clientItems = useMemo(
    () => clients.data?.pages.flatMap((page) => page.result.items) ?? [],
    [clients.data],
  );
  const clientsReady = clients.isSuccess && !draining && !clients.isError;
  const [draft, setDraft] = useState<APIKeyPolicyGrant[]>(
    apiKey.policyGrants ?? [],
  );
  const [newProject, setNewProject] = useState<string>(
    apiKey.projectId ?? organization.projects[0]?.id ?? "",
  );
  const [newClient, setNewClient] = useState<string>("");
  const servers = useOrgMcpServers(
    draft.some(
      (g) => grantKind(g) === "server" && g.selector.resourceId !== "*",
    ),
  );
  const serverNames = useMemo(() => {
    const names = new Map<string, string>();
    for (const group of servers.groups) {
      for (const server of group.servers) {
        names.set(server.id, `${server.name} (${group.projectName})`);
      }
    }
    return names;
  }, [servers.groups]);

  const clientNames = useMemo(() => {
    const names = new Map<string, { name: string; type: string }>();
    for (const client of clientItems) {
      names.set(client.id, { name: client.name, type: client.clientType });
    }
    return names;
  }, [clientItems]);

  // Keys never act for a person, so a person's own account is not offered.
  const offeredClients = clientItems.filter(
    (client) =>
      client.clientType !== "remote_session" &&
      !draft.some(
        (g) => grantKind(g) === "client" && g.selector.resourceId === client.id,
      ),
  );

  const update = useUpdateAPIKeyPolicyMutation({
    onSuccess: () => {
      toast.success(`Saved access for ${apiKey.name}`);
      void invalidateAllListAPIKeys(queryClient);
      onOpenChange(false);
    },
    onError: (error) =>
      toast.error(
        error.message.includes("outside this organization")
          ? "A server or credential in this list no longer exists. Remove it and save again."
          : "The access could not be saved. Try again.",
      ),
  });

  const describe = (grant: APIKeyPolicyGrant): string => {
    const s = grant.selector;
    if (grantKind(grant) === "client") {
      if (s.resourceId === "*") {
        return s.clientType
          ? `Every ${clientTypeLabel(s.clientType).toLowerCase()} credential`
          : "Every credential";
      }
      const client = clientNames.get(s.resourceId);
      if (client) return `${client.name} (${clientTypeLabel(client.type)})`;
      if (clients.isError) return "A credential that could not be loaded";
      return clientsReady ? "A removed credential" : "Loading credential…";
    }
    if (s.resourceId === "*") {
      return `Every server in ${projectBindingLabel(organization.projects, s.projectId)}`;
    }
    const server = serverNames.get(s.resourceId);
    if (server) return server;
    if (servers.isError)
      return `A server that could not be loaded (${s.resourceId})`;
    return servers.settled
      ? `A removed server (${s.resourceId})`
      : "Loading server…";
  };

  const setEffect = (target: APIKeyPolicyGrant, effect: Effect) =>
    setDraft((grants) =>
      grants.map((grant) =>
        grant === target
          ? {
              ...grant,
              scope:
                effect === "allow"
                  ? ALLOW_SCOPE[grantKind(grant)]
                  : BLOCK_SCOPE[grantKind(grant)],
            }
          : grant,
      ),
    );

  const changed =
    draft.map(grantKey).sort().join("\n") !==
    (apiKey.policyGrants ?? []).map(grantKey).sort().join("\n");

  return (
    <Sheet open onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] flex-col sm:max-w-[560px]"
      >
        <SheetHeader>
          <SheetTitle>MCP access for {apiKey.name}</SheetTitle>
          <SheetDescription>
            The servers this key may reach and the upstream credentials it may
            use there. The key holds only what is listed, never its
            creator&apos;s access. Only organization admins can change this.
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-4 pb-4">
          {draft.length === 0 ? (
            <Text muted small className="block">
              Nothing listed yet. Add the servers this key needs, then the
              credentials it may use on them.
            </Text>
          ) : (
            <ul className="border-border divide-y border">
              {draft.map((grant) => (
                <li
                  key={grantKey(grant)}
                  className="flex items-center gap-3 px-3 py-2"
                >
                  <div className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium">
                      {describe(grant)}
                    </span>
                    <Text muted small className="block">
                      {grantKind(grant) === "server"
                        ? "Server access"
                        : "Upstream credential"}
                    </Text>
                  </div>
                  <SegmentedControl<Effect>
                    ariaLabel={`Access to ${describe(grant)}`}
                    value={grantEffect(grant)}
                    onChange={(effect) => setEffect(grant, effect)}
                    options={[
                      { value: "allow", label: "Allowed" },
                      { value: "block", label: "Blocked" },
                    ]}
                  />
                  <Button
                    variant="tertiary"
                    size="sm"
                    aria-label={`Remove ${describe(grant)}`}
                    onClick={() =>
                      setDraft((grants) => grants.filter((g) => g !== grant))
                    }
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </li>
              ))}
            </ul>
          )}

          <section className="space-y-2">
            <h3 className="text-sm font-medium">Add server access</h3>
            <div className="flex gap-2">
              <Select
                value={newProject}
                onValueChange={setNewProject}
                disabled={!!apiKey.projectId}
              >
                <SelectTrigger className="flex-1">
                  <SelectValue placeholder="Choose a project" />
                </SelectTrigger>
                <SelectContent>
                  {organization.projects.map((project) => (
                    <SelectItem key={project.id} value={project.id}>
                      {project.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                variant="secondary"
                disabled={!newProject}
                onClick={() =>
                  setDraft((grants) => {
                    const next: APIKeyPolicyGrant = {
                      scope: APIKeyPolicyGrantScope.McpConnect,
                      selector: {
                        resourceKind: "mcp",
                        resourceId: "*",
                        projectId: newProject,
                      },
                    };
                    return grants.some((g) => grantKey(g) === grantKey(next))
                      ? grants
                      : [...grants, next];
                  })
                }
              >
                <Button.Text>Add every server</Button.Text>
              </Button>
            </div>
          </section>

          <section className="space-y-2">
            <h3 className="text-sm font-medium">Add a credential</h3>
            <Text muted small className="block">
              A key reaches an upstream only through a credential named here.
            </Text>
            <div className="flex gap-2">
              <Select
                value={newClient}
                onValueChange={setNewClient}
                disabled={!clientsReady}
              >
                <SelectTrigger className="flex-1">
                  <SelectValue
                    placeholder={
                      clients.isError
                        ? "Credentials could not be loaded"
                        : !clientsReady
                          ? "Loading credentials…"
                          : offeredClients.length === 0
                            ? "No more credentials to add"
                            : "Choose a credential"
                    }
                  />
                </SelectTrigger>
                <SelectContent>
                  {offeredClients.map((client) => (
                    <SelectItem key={client.id} value={client.id}>
                      {client.name} · {clientTypeLabel(client.clientType)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                variant="secondary"
                disabled={!newClient}
                onClick={() => {
                  setDraft((grants) => [
                    ...grants,
                    {
                      scope: APIKeyPolicyGrantScope.McpAuth,
                      selector: {
                        resourceKind: "mcp_auth_client",
                        resourceId: newClient,
                        // A project-bound key's grants must name its project.
                        projectId: apiKey.projectId,
                      },
                    },
                  ]);
                  setNewClient("");
                }}
              >
                <Button.Text>Add</Button.Text>
              </Button>
            </div>
          </section>
        </div>

        <SheetFooter className="border-t">
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            disabled={!changed || update.isPending}
            onClick={() =>
              update.mutate({
                request: {
                  updateKeyPolicyRequestBody: {
                    id: apiKey.id,
                    policyGrants: draft,
                  },
                },
              })
            }
          >
            <Button.Text>{update.isPending ? "Saving…" : "Save"}</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
