import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Cell,
  DeleteButton,
  Row,
  Section,
  Table,
} from "@/components/ema/Section";
import {
  useCreateEmaApp,
  useDeleteEmaApp,
  useUpdateEmaApp,
  useEmaApps,
} from "@/hooks/use-ema";

export const Route = createFileRoute("/ema/apps")({
  component: AppsPage,
});

function AppsPage() {
  const { data, isLoading } = useEmaApps();
  const create = useCreateEmaApp();
  const update = useUpdateEmaApp();
  const remove = useDeleteEmaApp();

  const [clientID, setClientID] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [jwks, setJwks] = useState("");
  const [name, setName] = useState("");

  const submit = () => {
    if (!clientID.trim()) return;
    create.mutate(
      {
        client_id: clientID.trim(),
        client_secret: clientSecret.trim() || undefined,
        jwks: jwks.trim() || undefined,
        name: name.trim() || undefined,
      },
      {
        onSuccess: () => {
          setClientID("");
          setClientSecret("");
          setJwks("");
          setName("");
        },
      },
    );
  };

  return (
    <Section
      title="Apps"
      description="Clients allowed to ask the IdP for an ID-JAG on a user's behalf. How an app authenticates follows from what it registers: a JWKS means private_key_jwt (what real enterprise IdPs mandate, and it wins over a secret), a secret alone means client_secret_post, neither means a public client. Registering an app grants it nothing on its own — that is what assignments are for."
    >
      <div className="flex flex-wrap items-end gap-3 rounded-lg border border-border p-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="app-client-id">Client ID</Label>
          <Input
            id="app-client-id"
            value={clientID}
            onChange={(e) => setClientID(e.target.value)}
            placeholder="my-mcp-client"
            className="w-56"
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="app-name">Name</Label>
          <Input
            id="app-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="defaults to the client id"
            className="w-56"
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="app-secret">Secret</Label>
          <Input
            id="app-secret"
            value={clientSecret}
            onChange={(e) => setClientSecret(e.target.value)}
            placeholder="blank = public client"
            className="w-56"
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="app-jwks">JWKS</Label>
          <Input
            id="app-jwks"
            value={jwks}
            onChange={(e) => setJwks(e.target.value)}
            placeholder='{"keys":[…]} — requires private_key_jwt'
            className="w-72"
          />
        </div>
        <Button
          onClick={submit}
          disabled={!clientID.trim() || create.isPending}
        >
          Register
        </Button>
      </div>

      {create.error ? (
        <p className="text-sm text-destructive">{String(create.error)}</p>
      ) : null}

      <Table
        headers={["Client ID", "Name", "Authenticates with", "Enabled", ""]}
        isEmpty={(data?.items ?? []).length === 0}
        empty={isLoading ? "Loading…" : "No apps registered."}
      >
        {(data?.items ?? []).map((app) => (
          <Row key={app.id}>
            <Cell mono>{app.client_id}</Cell>
            <Cell>{app.name}</Cell>
            <Cell mono>
              {app.jwks
                ? "private_key_jwt"
                : app.client_secret
                  ? "client_secret_post"
                  : "none (public)"}
            </Cell>
            <Cell>
              <Button
                variant="outline"
                size="sm"
                onClick={() =>
                  update.mutate({ id: app.id, enabled: !app.enabled })
                }
              >
                {app.enabled ? "Enabled" : "Disabled"}
              </Button>
            </Cell>
            <Cell className="text-right">
              <DeleteButton
                onClick={() => remove.mutate({ id: app.id })}
                disabled={remove.isPending}
              />
            </Cell>
          </Row>
        ))}
      </Table>
    </Section>
  );
}
