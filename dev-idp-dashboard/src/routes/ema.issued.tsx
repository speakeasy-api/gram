import { createFileRoute } from "@tanstack/react-router";

import { Cell, OrDash, Row, Section, Table } from "@/components/ema/Section";
import { useUsers } from "@/hooks/use-devidp";
import {
  useEmaApps,
  useEmaIssuedGrants,
  useEmaResources,
} from "@/hooks/use-ema";

export const Route = createFileRoute("/ema/issued")({
  component: IssuedPage,
});

function IssuedPage() {
  const { data, isLoading } = useEmaIssuedGrants();
  const apps = useEmaApps();
  const resources = useEmaResources();
  const users = useUsers();

  const appLabel = (id: string) =>
    apps.data?.items.find((a) => a.id === id)?.client_id ?? id;
  const userLabel = (id: string) =>
    users.data?.items.find((u) => u.id === id)?.email ?? id;
  const resourceLabel = (id: string) =>
    resources.data?.items.find((r) => r.id === id)?.slug ?? id;

  return (
    <Section
      title="Issued grants"
      description="Every ID-JAG this IdP has minted, newest first. Read-only: it records what policy actually allowed, which is the fastest way to see why a redemption behaved the way it did. Redemption is tracked separately, since a resource may accept grants from issuers other than this one."
    >
      <Table
        headers={[
          "Minted",
          "App",
          "User",
          "Resource",
          "Scope",
          "Expires",
          "jti",
        ]}
        isEmpty={(data?.items ?? []).length === 0}
        empty={isLoading ? "Loading…" : "Nothing minted yet."}
      >
        {(data?.items ?? []).map((g) => (
          <Row key={g.jti}>
            <Cell mono>{g.created_at}</Cell>
            <Cell mono>{appLabel(g.app_id)}</Cell>
            <Cell>{userLabel(g.user_id)}</Cell>
            <Cell mono>{resourceLabel(g.resource_id)}</Cell>
            <Cell mono>
              <OrDash value={g.scope} />
            </Cell>
            <Cell mono>{g.expires_at}</Cell>
            <Cell mono className="opacity-60">
              {g.jti.slice(0, 8)}
            </Cell>
          </Row>
        ))}
      </Table>
    </Section>
  );
}
