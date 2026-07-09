import type { Meta, StoryObj } from "@storybook/react-vite";

import { Badge, Button, Table, type Column } from "@/components/ui/moonshine";
import { Card } from "@/components/ui/card";
import { DetailList } from "@/components/ui/detail-list";
import { LoadMoreFooter } from "@/components/ui/load-more-footer";
import { StatTile } from "@/components/ui/stat-tile";
import { StatusDot } from "@/components/ui/status-dot";
import { Type } from "@/components/ui/type";
import { DetailLayout } from "./detail-layout";
import { ListLayout } from "./list-layout";
import { ObservabilityLayout } from "./observability-layout";
import { SettingsLayout } from "./settings-layout";

const meta: Meta = {
  title: "Layouts/Page Layouts",
  parameters: { layout: "fullscreen" },
  decorators: [
    (Story) => (
      <div className="bg-surface-primary-default min-h-screen p-8">
        <Story />
      </div>
    ),
  ],
};

export default meta;

interface Server {
  name: string;
  tools: number;
  status: "Online" | "Degraded";
}

const servers: Server[] = [
  { name: "github", tools: 24, status: "Online" },
  { name: "linear", tools: 12, status: "Online" },
  { name: "postgres", tools: 8, status: "Degraded" },
];

const serverColumns: Column<Server>[] = [
  { key: "name", header: "Server", width: "1fr" },
  {
    key: "tools",
    header: "Tools",
    width: "0.5fr",
    render: (row) => <Type mono>{row.tools}</Type>,
  },
  {
    key: "status",
    header: "Status",
    width: "0.5fr",
    render: (row) => (
      <StatusDot
        tone={row.status === "Online" ? "success" : "warning"}
        label={row.status}
      />
    ),
  },
];

export const List: StoryObj = {
  render: () => (
    <ListLayout>
      <ListLayout.Header
        title="MCP servers"
        subtitle="Every tool surface your agents can reach."
        actions={<Button>Add server</Button>}
      />
      <ListLayout.List>
        <Table columns={serverColumns} data={servers} rowKey={(r) => r.name} />
      </ListLayout.List>
      <ListLayout.Footer>
        <LoadMoreFooter
          shown={3}
          total={12}
          noun="servers"
          hasMore
          onLoadMore={() => {}}
        />
      </ListLayout.Footer>
    </ListLayout>
  ),
};

export const Detail: StoryObj = {
  render: () => (
    <DetailLayout>
      <DetailLayout.Header
        eyebrow="MCP Server"
        title="github"
        subtitle="Repository and pull-request tools, scoped to engineering."
        actions={
          <>
            <Button variant="secondary">Edit</Button>
            <Button>Deploy</Button>
          </>
        }
      />
      <DetailLayout.Content>
        <DetailLayout.Main>
          <DetailLayout.Section title="Tools" annotation="24 exposed">
            <Card>
              <Card.Header>
                <Card.Title>create_pull_request</Card.Title>
                <Badge dot variant="success">
                  Enabled
                </Badge>
              </Card.Header>
              <Card.Content>
                <Type muted small>
                  Opens a pull request against a branch in the connected
                  repository.
                </Type>
              </Card.Content>
            </Card>
          </DetailLayout.Section>
        </DetailLayout.Main>
        <DetailLayout.Aside>
          <DetailList orientation="stacked">
            <DetailList.Item label="Environment" value="production" />
            <DetailList.Item label="Deployed" value="2 hours ago" />
            <DetailList.Item label="Owner" value="platform@acme.com" />
          </DetailList>
        </DetailLayout.Aside>
      </DetailLayout.Content>
    </DetailLayout>
  ),
};

export const Observability: StoryObj = {
  render: () => (
    <ObservabilityLayout>
      <ObservabilityLayout.Header
        eyebrow="Risk · Watchdog"
        title="Watchdog"
        subtitle="Your riskiest AI usage, clustered and ranked. 8 open · 2 critical need action."
        actions={
          <>
            <Button variant="secondary">24H</Button>
            <Button>Export report</Button>
          </>
        }
      />
      <ObservabilityLayout.Stats>
        <StatTile
          label="Org risk score"
          value="78"
          delta={{ value: "+6", tone: "negative" }}
          caption="High — driven by 2 critical signals"
          tone="destructive"
        />
        <StatTile
          label="Findings · last 24h"
          value="3,418"
          delta={{ value: "+18%", tone: "negative" }}
          caption="across 8 clustered signals"
        />
        <StatTile
          label="Open signals"
          value="8"
          delta={{ value: "2 crit", tone: "negative" }}
          caption="unresolved & ranked by risk"
        />
        <StatTile
          label="Users exposed"
          value="86"
          delta={{ value: "+12", tone: "negative" }}
          caption="of 540 active seats"
        />
      </ObservabilityLayout.Stats>
      <ObservabilityLayout.Strip
        label="Exposure by data type · last 24h"
        annotation="3,418 findings"
      >
        <div className="flex h-2 w-full">
          <div className="bg-lang-javascript w-[42%]" />
          <div className="bg-lang-csharp w-[21%]" />
          <div className="bg-lang-ruby w-[18%]" />
          <div className="bg-lang-typescript w-[11%]" />
          <div className="bg-lang-rust w-[5%]" />
          <div className="bg-lang-python w-[3%]" />
        </div>
      </ObservabilityLayout.Strip>
      <ObservabilityLayout.Section title="Active signals" annotation="8 of 8">
        <ObservabilityLayout.Grid columns={2}>
          <Card>
            <Card.Header>
              <Card.Title>AWS credentials pasted into Codex</Card.Title>
              <Badge dot variant="destructive">
                Critical
              </Badge>
            </Card.Header>
            <Card.Content>
              <Type muted small>
                Long-lived AWS access keys with admin scope were sent to the
                model in plaintext.
              </Type>
            </Card.Content>
          </Card>
          <Card>
            <Card.Header>
              <Card.Title>Production DB strings in agent logs</Card.Title>
              <Badge dot variant="destructive">
                Critical
              </Badge>
            </Card.Header>
            <Card.Content>
              <Type muted small>
                Postgres connection URIs appear in custom-agent tool calls.
              </Type>
            </Card.Content>
          </Card>
        </ObservabilityLayout.Grid>
      </ObservabilityLayout.Section>
    </ObservabilityLayout>
  ),
};

export const Settings: StoryObj = {
  render: () => (
    <SettingsLayout>
      <SettingsLayout.Header
        title="Settings"
        subtitle="How this organization authenticates, bills, and reports."
      />
      <SettingsLayout.Body>
        <SettingsLayout.Group
          label="Authentication"
          description="How agents prove who they are."
          actions={<Button variant="secondary">Configure</Button>}
        >
          <DetailList orientation="inline">
            <DetailList.Item label="Provider" value="Okta" />
            <DetailList.Item label="Enforced" value="All members" />
          </DetailList>
        </SettingsLayout.Group>
        <SettingsLayout.Group
          label="Telemetry"
          description="Where tool-call logs are shipped."
        >
          <DetailList orientation="inline">
            <DetailList.Item label="Destination" value="Datadog" />
            <DetailList.Item label="Retention" value="30 days" />
          </DetailList>
        </SettingsLayout.Group>
        <SettingsLayout.DangerZone description="These actions cannot be undone.">
          <Button variant="destructive-primary">Delete organization</Button>
        </SettingsLayout.DangerZone>
      </SettingsLayout.Body>
    </SettingsLayout>
  ),
};
