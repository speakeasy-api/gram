import { Gram } from "@gram/client";
import { GramProvider } from "@gram/client/react-query/_context.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Decorator, Meta, StoryObj } from "@storybook/react-vite";
import { MemoryRouter } from "react-router";

import { SidebarInset, SidebarProvider } from "@/components/ui/Sidebar";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Column, Table } from "@/components/ui/Table";
import {
  DangerSettingsSection,
  DetailPage,
  FormPage,
  FooterSaveButton,
  OverviewPage,
  ResourceListPage,
  SettingsPage,
  SettingsSection,
  TabbedPage,
  WizardPage,
  WorkbenchPage,
} from "./index";

/**
 * The templates render the real app frame (breadcrumbs, RBAC crumb check via
 * useGrants, sidebar trigger), so each story mounts inside the minimal
 * provider stack the frame needs. The Gram client points at an unreachable
 * host: the one query the frame issues (grants) fails silently and the org
 * crumb simply renders unlinked. Auth/Telemetry/Insights contexts all have
 * safe defaults and need no providers here.
 */
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: Infinity },
  },
});
const gram = new Gram({ serverURL: "http://storybook.invalid" });

function appFrame(path: string): Decorator {
  return (Story) => (
    <MemoryRouter initialEntries={[path]}>
      <QueryClientProvider client={queryClient}>
        <GramProvider client={gram}>
          <SidebarProvider>
            <SidebarInset>
              <Story />
            </SidebarInset>
          </SidebarProvider>
        </GramProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
}

const meta: Meta = {
  title: "Design System/Page Templates/Examples",
  parameters: { layout: "fullscreen" },
};
export default meta;

type Story = StoryObj;

// --- ResourceListPage -------------------------------------------------------

type Widget = { id: string; name: string; kind: string; createdAt: string };

const widgets: Widget[] = [
  { id: "1", name: "billing-sync", kind: "OpenAPI", createdAt: "2026-07-02" },
  { id: "2", name: "crm-lookup", kind: "Function", createdAt: "2026-07-18" },
  {
    id: "3",
    name: "search-index",
    kind: "Remote MCP",
    createdAt: "2026-08-01",
  },
];

const widgetColumns: Column<Widget>[] = [
  { key: "name", header: "Name", render: (w) => w.name },
  { key: "kind", header: "Kind", render: (w) => w.kind },
  { key: "created", header: "Created", render: (w) => w.createdAt },
];

const newWidgetButton = (
  <Button>
    <Button.Text>New widget</Button.Text>
  </Button>
);

export const ResourceList: Story = {
  decorators: [appFrame("/acme/projects/default/mcp")],
  render: () => (
    <ResourceListPage
      title="MCP Servers"
      description="Servers exposed to your agents."
      primaryAction={newWidgetButton}
      search={{ value: "", onChange: () => {}, placeholder: "Search servers" }}
      metrics={[
        { label: "Servers", value: 3, tone: "information" },
        { label: "Healthy", value: 3, tone: "success" },
        {
          label: "Errors",
          value: 0,
          tone: "neutral",
          description: "last 24 h",
        },
      ]}
      isEmpty={false}
      empty={{ icon: "blocks", heading: "No servers yet" }}
    >
      <Table columns={widgetColumns} data={widgets} rowKey={(r) => r.id} />
    </ResourceListPage>
  ),
};

export const ResourceListEmpty: Story = {
  decorators: [appFrame("/acme/projects/default/mcp")],
  render: () => (
    <ResourceListPage
      title="MCP Servers"
      description="Servers exposed to your agents."
      isEmpty
      empty={{
        icon: "blocks",
        heading: "No servers yet",
        description: "Connect a source to expose your first server.",
        action: newWidgetButton,
      }}
    >
      {null}
    </ResourceListPage>
  ),
};

export const ResourceListLoading: Story = {
  decorators: [appFrame("/acme/projects/default/mcp")],
  render: () => (
    <ResourceListPage
      title="MCP Servers"
      description="Servers exposed to your agents."
      isLoading
    >
      {null}
    </ResourceListPage>
  ),
};

// --- DetailPage -------------------------------------------------------------

const sectionCard = (text: string) => (
  <div className="bg-card border p-6">{text}</div>
);

export const Detail: Story = {
  decorators: [appFrame("/acme/projects/default/mcp/prod-server/overview")],
  render: () => (
    <DetailPage
      title="prod-server"
      description="One entity, sections as routed tabs."
      breadcrumbSubstitutions={{ "prod-server": "Production Server" }}
      activeSection="overview"
      sections={[
        {
          id: "overview",
          label: "Overview",
          href: "/acme/projects/default/mcp/prod-server/overview",
          content: sectionCard("Overview section content."),
        },
        {
          id: "settings",
          label: "Settings",
          href: "/acme/projects/default/mcp/prod-server/settings",
          content: sectionCard("Settings section content."),
        },
      ]}
    />
  ),
};

export const DetailLoading: Story = {
  decorators: [appFrame("/acme/projects/default/mcp/prod-server")],
  render: () => <DetailPage loading sections={[]} />,
};

export const DetailNotFound: Story = {
  decorators: [appFrame("/acme/projects/default/mcp/prod-server")],
  render: () => (
    <DetailPage
      sections={[]}
      notFound={{
        title: "Server not found",
        description: "It may have been deleted.",
        backTo: "/acme/projects/default/mcp",
      }}
    />
  ),
};

// --- TabbedPage -------------------------------------------------------------

export const Tabbed: Story = {
  decorators: [appFrame("/acme/projects/default/access/roles")],
  render: () => (
    <TabbedPage
      title="Access"
      description="Tabs are different resources, not one entity's sections."
      activeTab="roles"
      tabs={[
        {
          value: "roles",
          label: "Roles",
          href: "/acme/projects/default/access/roles",
        },
        {
          value: "members",
          label: "Members",
          href: "/acme/projects/default/access/members",
        },
      ]}
    >
      {sectionCard("Roles tab content.")}
    </TabbedPage>
  ),
};

// --- FormPage ---------------------------------------------------------------

export const Form: Story = {
  decorators: [appFrame("/acme/projects/default/prompts/new")],
  render: () => (
    <FormPage title="New prompt" description="A single create/edit form.">
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => e.preventDefault()}
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="pt-name">Name</Label>
          <Input id="pt-name" placeholder="my-prompt" />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="pt-secret">API key</Label>
          <Input id="pt-secret" type="password" reveal placeholder="sk-…" />
        </div>
        <div>
          <Button type="submit">
            <Button.Text>Create</Button.Text>
          </Button>
        </div>
      </form>
    </FormPage>
  ),
};

// --- SettingsPage -----------------------------------------------------------

export const Settings: Story = {
  decorators: [appFrame("/acme/projects/default/settings")],
  render: () => (
    <SettingsPage
      title="Project settings"
      description="Stacked titled config sections."
    >
      <SettingsSection>
        <SettingsSection.Header>
          <SettingsSection.Title>General</SettingsSection.Title>
          <SettingsSection.Description>
            Name and description shown across the dashboard.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <div className="flex max-w-md flex-col gap-2">
              <Label htmlFor="pt-project-name">Project name</Label>
              <Input id="pt-project-name" defaultValue="default" />
            </div>
          </SettingsSection.Body>
          <SettingsSection.Footer>
            <SettingsSection.FooterActions>
              <FooterSaveButton pending={false} />
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        </SettingsSection.Panel>
      </SettingsSection>
      <DangerSettingsSection>
        <DangerSettingsSection.Header>
          <DangerSettingsSection.Title>Danger zone</DangerSettingsSection.Title>
          <DangerSettingsSection.Description>
            Irreversible actions.
          </DangerSettingsSection.Description>
        </DangerSettingsSection.Header>
        <DangerSettingsSection.Panel>
          <DangerSettingsSection.Body>
            <Button variant="destructive-primary">
              <Button.Text>Delete project</Button.Text>
            </Button>
          </DangerSettingsSection.Body>
        </DangerSettingsSection.Panel>
      </DangerSettingsSection>
    </SettingsPage>
  ),
};

// --- OverviewPage -----------------------------------------------------------

export const Overview: Story = {
  decorators: [appFrame("/acme/projects/default/logs")],
  render: () => (
    <OverviewPage
      title="Risk Overview"
      description="A stat row over summary cards."
      metrics={[
        { label: "Signals", value: 128, tone: "information" },
        { label: "Open findings", value: 4, tone: "destructive", delta: "+2" },
        { label: "Policies", value: 12, tone: "neutral" },
      ]}
    >
      <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
        {sectionCard("Chart card A.")}
        {sectionCard("Chart card B.")}
      </div>
    </OverviewPage>
  ),
};

// --- WorkbenchPage ----------------------------------------------------------

export const Workbench: Story = {
  decorators: [appFrame("/acme/projects/default/logs")],
  render: () => (
    <WorkbenchPage>
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="shrink-0 border-b p-4">Sticky filter bar</div>
        <div className="min-h-0 flex-1 overflow-y-auto p-4">
          Big table / charts area with internal scroll.
        </div>
      </div>
    </WorkbenchPage>
  ),
};

// --- WizardPage -------------------------------------------------------------

export const Wizard: Story = {
  decorators: [appFrame("/acme/projects/default/onboarding")],
  render: () => (
    <WizardPage
      currentStepId="configure"
      steps={[
        { id: "source", title: "Pick a source", description: "Done" },
        { id: "configure", title: "Configure", description: "You are here" },
        { id: "review", title: "Review" },
      ]}
    >
      {sectionCard("Active step body.")}
    </WizardPage>
  ),
};
