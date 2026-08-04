import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Cell,
  DeleteButton,
  OrDash,
  Row,
  Section,
  Table,
} from "@/components/ema/Section";
import {
  useCreateEmaTrustRule,
  useDeleteEmaTrustRule,
  useUpdateEmaTrustRule,
  useEmaResources,
  useEmaTrustRules,
} from "@/hooks/use-ema";

export const Route = createFileRoute("/ema/trust-rules")({
  component: TrustRulesPage,
});

function TrustRulesPage() {
  const { data, isLoading } = useEmaTrustRules();
  const resources = useEmaResources();
  const create = useCreateEmaTrustRule();
  const update = useUpdateEmaTrustRule();
  const remove = useDeleteEmaTrustRule();

  const [resourceID, setResourceID] = useState("");
  const [issuer, setIssuer] = useState("");
  const [scopes, setScopes] = useState("");

  const resourceLabel = (id: string) =>
    resources.data?.items.find((r) => r.id === id)?.slug ?? id;

  const submit = () => {
    if (!resourceID || !issuer.trim()) return;
    create.mutate(
      {
        resource_id: resourceID,
        trusted_issuer: issuer.trim(),
        allowed_scopes: scopes.trim() || undefined,
      },
      {
        onSuccess: () => {
          setIssuer("");
          setScopes("");
        },
      },
    );
  };

  return (
    <Section
      title="Trust rules"
      description="Which issuer a resource accepts ID-JAGs from, and the scope ceiling it applies on top of whatever the grant already carries. This is enforced separately from assignments, so a grant this dev-idp minted can still be refused here. The issuer need not be this dev-idp — point one at a foreign IdP and its metadata and JWKS get fetched like any other."
    >
      <div className="flex flex-wrap items-end gap-3 rounded-lg border border-border p-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="trust-resource">Resource</Label>
          <select
            id="trust-resource"
            value={resourceID}
            onChange={(e) => setResourceID(e.target.value)}
            className="h-9 w-44 rounded-md border border-border bg-background px-2 text-sm"
          >
            <option value="">Select…</option>
            {(resources.data?.items ?? []).map((r) => (
              <option key={r.id} value={r.id}>
                {r.slug}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="trust-issuer">Trusted issuer</Label>
          <Input
            id="trust-issuer"
            value={issuer}
            onChange={(e) => setIssuer(e.target.value)}
            placeholder="http://localhost:35291/oauth2-1"
            className="w-72"
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="trust-scopes">Scope ceiling</Label>
          <Input
            id="trust-scopes"
            value={scopes}
            onChange={(e) => setScopes(e.target.value)}
            placeholder="blank = no ceiling"
            className="w-52"
          />
        </div>
        <Button
          onClick={submit}
          disabled={!resourceID || !issuer.trim() || create.isPending}
        >
          Trust
        </Button>
      </div>

      {create.error ? (
        <p className="text-sm text-destructive">{String(create.error)}</p>
      ) : null}

      <Table
        headers={[
          "Resource",
          "Trusted issuer",
          "Ceiling",
          "Clients",
          "Enabled",
          "",
        ]}
        isEmpty={(data?.items ?? []).length === 0}
        empty={
          isLoading
            ? "Loading…"
            : "No trust rules — every resource refuses every grant."
        }
      >
        {(data?.items ?? []).map((r) => (
          <Row key={r.id}>
            <Cell mono>{resourceLabel(r.resource_id)}</Cell>
            <Cell mono>{r.trusted_issuer}</Cell>
            <Cell mono>
              <OrDash value={r.allowed_scopes} />
            </Cell>
            <Cell mono>
              {r.allowed_client_ids === "[]" ? (
                <span className="opacity-40">any</span>
              ) : (
                r.allowed_client_ids
              )}
            </Cell>
            <Cell>
              <Button
                variant="outline"
                size="sm"
                onClick={() => update.mutate({ id: r.id, enabled: !r.enabled })}
              >
                {r.enabled ? "Enabled" : "Disabled"}
              </Button>
            </Cell>
            <Cell className="text-right">
              <DeleteButton
                onClick={() => remove.mutate({ id: r.id })}
                disabled={remove.isPending}
              />
            </Cell>
          </Row>
        ))}
      </Table>
    </Section>
  );
}
