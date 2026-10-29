import React from "react";
import { Navigate, Outlet } from "react-router";
import type {
  AppRoute,
  AppRoutes,
  OrgRouteStructure,
  RouteStructure,
} from "./routes";
import AssistantPage from "./pages/assistants/Assistant";
import AssistantsIndex, { AssistantsRoot } from "./pages/assistants/Assistants";
import NewAssistantPage from "./pages/assistants/NewAssistant";
import Billing from "./pages/billing/Billing";
import {
  BillingSetup,
  BillingPaymentMethod,
} from "./pages/billing/BillingSetup";
import Catalog, { CatalogRoot } from "./pages/catalog/Catalog";
import CatalogDetail, {
  CatalogDetailRoot,
} from "./pages/catalog/CatalogDetail";
import ChatSessions from "./pages/chatLogs/ChatLogs";
import OrgMemory from "./components/observe/OrgMemory";
import { ChatConversation, ChatHome, ChatRoot } from "./pages/chat/Chat";
import Skills from "./pages/Skills";
import SkillsList from "./pages/skills/SkillsList";
import SkillContent from "./pages/skills/SkillContent";
import SkillDetailRoot from "./pages/skills/SkillDetailRoot";
import SkillFeedback from "./pages/skills/SkillFeedback";
import SkillOverview from "./pages/skills/SkillOverview";
import SkillScoredSessions from "./pages/skills/SkillScoredSessions";
import SkillSettings from "./pages/skills/SkillSettings";
import SkillUsage from "./pages/skills/SkillUsage";
import SkillVersionHistory from "./pages/skills/SkillVersionHistory";
import Deployment from "./pages/deployments/deployment/Deployment";
import Deployments, { DeploymentsRoot } from "./pages/deployments/Deployments";
import ProjectIdentityRedirect from "./pages/org/ProjectIdentityRedirect";
import UserSessions from "./pages/org/UserSessions";
import EventFeed from "./pages/data/EventFeed";
import DataExports from "./pages/data-exports/DataExports";
import { LegacyDataRedirect } from "./pages/data-exports/LegacyDataRedirect";
import DeviceAgent, { DeviceAgentRoot } from "./pages/device-agent/DeviceAgent";
import MdmIntegrationDetail from "./pages/org/device-integrations/MdmIntegrationDetail";
import EnvironmentPage from "./pages/environments/Environment";
import Environments, {
  EnvironmentsRoot,
} from "./pages/environments/Environments";
import Home from "./pages/home/Home";
import { ProjectGuidePage } from "./components/project-guide/ProjectGuidePage";
import Integrations from "./pages/integrations/Integrations";
import Login from "./pages/login/Login";
import ExploreDemo from "./pages/demo/ExploreDemo";
import SignUp from "./pages/login/SignUp";
import { LogsRoot } from "./pages/logs/Logs";
import { MCPPage, MCPRoot } from "./pages/mcp/MCP";
import AddMcpServer, { AddMcpServerRoot } from "./pages/mcp/add/AddMcpServer";
import CreateFromSource from "./pages/mcp/add/CreateFromSource";
import { RedirectToDeployments } from "./pages/mcp/add/LegacyRedirects";
import Sources, { SourcesRoot } from "./pages/mcp/sources/Sources";
import SourceDetailRoute from "./pages/mcp/sources/SourceDetail";
import GatewayDetailPage from "./pages/mcp/gateway/GatewayDetails";
import OrganizationGatewayPage from "./pages/mcp/OrganizationGateway";
import MCPServerDetails from "./pages/mcp/x/MCPServerDetails";
import { ToolsetSlugRedirectPage } from "./pages/mcp/ToolsetRedirect";
import { InsightsHooksPage, InsightsRoot } from "./pages/insights/Insights";
import Costs from "./pages/costs/Costs";
import {
  BuiltInDashboardRoute,
  DashboardRoute,
  DashboardsIndex,
  DashboardsRoot,
} from "./pages/explore/Dashboards";
import Explore from "./pages/explore/Explore";
import SignalsIntelligenceRoute from "./components/signals-intelligence-route";
import {
  AgentsIndex,
  PeopleIndex,
  PeopleIndexRedirect,
  IdentityDetailIndexRedirect,
  IdentitiesRoot,
} from "./pages/identities/IdentitiesIndex";
import IdentityDetailRoot from "./pages/identities/IdentityDetailRoot";
import IdentityOverview from "./pages/identities/IdentityOverview";
import IdentityAccess from "./pages/identities/IdentityAccess";
import IdentityUsage from "./pages/identities/IdentityUsage";
import IdentitySecurity from "./pages/identities/IdentitySecurity";
import IdentityFindingsPage from "./pages/identities/IdentityFindings";
import IdentityCost from "./pages/identities/IdentityCost";
import IdentityDevices from "./pages/identities/IdentityDevices";
import IdentityConnections from "./pages/identities/IdentityConnections";
import IdentityActivity from "./pages/identities/IdentityActivity";
import IdentityAgentSection from "./pages/identities/IdentityAgentSection";
import NewAgentIdentity from "./pages/agents/NewAgentIdentity";
import EditAgentIdentity from "./pages/agents/EditAgentIdentity";
import AgentManagementRedirect from "./pages/agents/AgentManagementRedirect";
import FunctionsOnboarding from "./pages/onboarding/FunctionsOnboarding";
import UploadOpenAPI from "./pages/onboarding/UploadOpenAPI";
import CreateRemoteMcp from "./pages/sources/remote-mcp/CreateRemoteMcp";
import CreateTunneledMcp from "./pages/sources/tunneled-mcp/CreateTunneledMcp";
import OrgApiKeys from "./pages/org/OrgApiKeys";
import Plugins, { PluginsRoot } from "./pages/plugins/Plugins";
import PluginDetail from "./pages/plugins/PluginDetail";
import OrgAuditLogs from "./pages/org/OrgAuditLogs";
import OrgDomains from "./pages/org/OrgDomains";
import OrgHome from "./pages/org/OrgHome";
import OrgIdentity from "./pages/org/OrgIdentity";
import OrgAIIntegrations from "./pages/org/OrgAIIntegrations";
import OrgLogs from "./pages/org/OrgLogs";
import OrgGateway from "./pages/org/OrgGateway";
import HeadlessMode from "./pages/org/HeadlessMode";
import PlatformMCPRedirect from "./pages/org/PlatformMCPRedirect";
import OrgSkills from "./pages/org/OrgSkills";
import ExternalCredentialDetail from "./pages/org/external-services/ExternalCredentialDetail";
import {
  ExternalServicesPage,
  ExternalServicesRoot,
} from "./pages/org/external-services/ExternalServices";
import ExternalKeyDetail from "./pages/org/encryption-keys/ExternalKeyDetail";
import JsonWebKeySetDetail, {
  SigningKeySetsIndex,
  SigningKeySetsRoot,
} from "./pages/org/encryption-keys/jwks/JsonWebKeySetDetail";
import {
  EncryptionKeysPage,
  EncryptionKeysRoot,
} from "./pages/org/encryption-keys/EncryptionKeys";
import OrgWebhooks from "./pages/org/OrgWebhooks";
import UserSessionIssuerDetail from "./pages/user-session-issuers/UserSessionIssuerDetail";
import {
  UserSessionIssuersPage,
  UserSessionIssuersRoot,
} from "./pages/user-session-issuers/UserSessionIssuers";
import {
  RemoteIdentityProvidersPage,
  RemoteIdentityProvidersRoot,
} from "./pages/remote-identity-providers/RemoteIdentityProviders";
import RemoteIdentityProviderDetail from "./pages/remote-identity-providers/RemoteIdentityProviderDetail";
import RemoteSessionClientDetail from "./pages/remote-identity-providers/RemoteSessionClientDetail";
import PlatformAdminOverview from "./pages/platform-admin/Overview";
import PlatformAdminRbacOverride from "./pages/platform-admin/RbacOverride";
import PlatformAdminOnboarding from "./pages/platform-admin/Onboarding";
import PlatformAdminOpenRouterKeys from "./pages/platform-admin/OpenRouterKeys";
import Playground from "./pages/playground/Playground";
import NewPromptPage from "./pages/prompts/NewPrompt";
import PromptPage from "./pages/prompts/Prompt";
import Prompts, { PromptsRoot } from "./pages/prompts/Prompts";
import SDK from "./pages/sdk/SDK";
import Access from "./pages/access/Access";
import { RoleEditorPage } from "./pages/access/RoleEditorPage";
import RequestAccess from "./pages/access/RequestAccess";
import Settings from "./pages/settings/Settings";
import TriggersIndex, { TriggersRoot } from "./pages/triggers/Triggers";
import RiskOverviewRedirect from "./pages/security/RiskOverviewRedirect";
import Watchdog from "./pages/security/watchdog/Watchdog";
import RiskEventsPage from "./pages/security/RiskEventsPage";
import {
  ShadowAIRoot,
  ShadowAISectionRedirect,
  ShadowMCPIndexRedirect,
  ShadowMCPServerLegacyRedirect,
} from "./pages/shadow-ai/ShadowAI";
import ShadowAIToolDetail from "./pages/shadow-ai/ShadowAIToolDetail";
import ShadowMCPServerDetail from "./pages/shadow-mcp/ShadowMCPServerDetail";
import PolicyCenter, { PolicyCenterRoot } from "./pages/security/PolicyCenter";
import PolicyDetail, { PolicyNew } from "./pages/security/PolicyDetail";
import DetectionRules from "./pages/security/DetectionRules";
import Team from "./pages/team/Team";
import {
  KillswitchesRoot,
  KillswitchIndexRedirect,
  KillswitchRecordRedirect,
} from "./pages/killswitch/KillswitchesRoot";

const SetupWizard = React.lazy(() => import("./pages/setup/SetupWizard"));
const SetupTaskRedirect = React.lazy(
  () => import("./pages/setup/SetupTaskRedirect"),
);

/**
 * The page each route renders. Route paths, titles and the `useRoutes` /
 * `useOrgRoutes` hooks live in `routes.tsx` with no page imports, because
 * shared components import them to build links. This map imports every page,
 * so only the app shell should import it.
 *
 * Every route needs an entry, even one with no page of its own (external links,
 * tabs rendered by their parent), so a new route cannot be left unmapped.
 */
type RoutePages = {
  component?: React.ComponentType;
  indexComponent?: React.ComponentType;
};

type PageTree<T> = {
  [K in keyof T]: RoutePages &
    (T[K] extends { subPages: infer S }
      ? { subPages: PageTree<S> }
      : { subPages?: never });
};

type AnyPageTree = Record<string, RoutePages & { subPages?: AnyPageTree }>;

/** A route with the page it renders, as mounted by the app shell. */
export type PagedRoute = AppRoute & RoutePages;

const ROUTE_PAGES: PageTree<RouteStructure> = {
  login: {
    component: Login,
  },
  register: {
    component: () => <Navigate to="/sign-up" replace />,
  },
  exploreDemo: {
    component: ExploreDemo,
  },
  signUp: {
    component: SignUp,
  },
  home: {
    component: Home,
  },
  guide: {
    component: ProjectGuidePage,
  },
  chat: {
    component: ChatRoot,
    indexComponent: ChatHome,
    subPages: {
      conversation: {
        component: ChatConversation,
      },
    },
  },
  playground: {
    component: Playground,
  },
  integrations: {
    component: Integrations,
  },
  prompts: {
    component: PromptsRoot,
    indexComponent: Prompts,
    subPages: {
      newPrompt: {
        component: NewPromptPage,
      },
      prompt: {
        component: PromptPage,
      },
    },
  },
  assistants: {
    component: AssistantsRoot,
    indexComponent: AssistantsIndex,
    subPages: {
      newAssistant: {
        component: NewAssistantPage,
      },
      detail: {
        component: AssistantPage,
      },
    },
  },
  signalsIntelligence: {
    component: SignalsIntelligenceRoute,
  },
  skills: {
    component: Skills,
    indexComponent: SkillsList,
    subPages: {
      detail: {
        component: SkillDetailRoot,
        subPages: {
          overview: {
            component: SkillOverview,
          },
          content: {
            component: SkillContent,
          },
          usage: {
            component: SkillUsage,
          },
          scoredSessions: {
            component: SkillScoredSessions,
          },
          feedback: {
            component: SkillFeedback,
          },
          versions: {
            component: SkillVersionHistory,
            subPages: {
              version: {
                component: SkillVersionHistory,
              },
            },
          },
          settings: {
            component: SkillSettings,
          },
        },
      },
    },
  },
  mcp: {
    component: MCPRoot,
    indexComponent: MCPPage,
    subPages: {
      add: {
        component: AddMcpServerRoot,
        indexComponent: AddMcpServer,
        subPages: {
          remote: {
            component: CreateRemoteMcp,
          },
          tunneled: {
            component: CreateTunneledMcp,
          },
          fromSource: {
            component: CreateFromSource,
          },
          openapi: {
            component: UploadOpenAPI,
          },
          function: {
            component: FunctionsOnboarding,
          },
        },
      },
      catalog: {
        component: CatalogRoot,
        indexComponent: Catalog,
        subPages: {
          detail: {
            component: CatalogDetailRoot,
            indexComponent: CatalogDetail,
          },
        },
      },
      deployments: {
        component: Deployments,
      },
      sources: {
        component: SourcesRoot,
        indexComponent: Sources,
        subPages: {
          detail: {
            component: SourceDetailRoute,
          },
        },
      },
      organizationGateway: {
        component: OrganizationGatewayPage,
      },
      gateway: {
        component: GatewayDetailPage,
        subPages: {
          overview: {},
          members: {},
          inspect: {},
          skills: {},
          teamAccess: {},
          sessions: {},
          settings: {},
        },
      },
      details: {
        component: MCPServerDetails,
        subPages: {
          overview: {},
          inspect: {},
          tools: {},
          resources: {},
          prompts: {},
          skills: {},
          teamAccess: {},
          guardrails: {},
          sessions: {},
          settings: {},
          authentication: {},
          performance: {},
        },
      },
    },
  },
  toolset: {
    component: ToolsetSlugRedirectPage,
    subPages: {
      overview: {},
      tools: {},
      resources: {},
      prompts: {},
      inspect: {},
      teamAccess: {},
      guardrails: {},
      sessions: {},
      settings: {},
      authentication: {},
      performance: {},
    },
  },
  environments: {
    component: EnvironmentsRoot,
    indexComponent: Environments,
    subPages: {
      environment: {
        component: EnvironmentPage,
      },
    },
  },
  triggers: {
    component: TriggersRoot,
    indexComponent: TriggersIndex,
  },
  insights: {
    component: InsightsRoot,
    indexComponent: InsightsHooksPage,
  },
  mcpSessions: {
    component: UserSessions,
  },
  remoteIdentityProviders: {
    component: RemoteIdentityProvidersRoot,
    indexComponent: RemoteIdentityProvidersPage,
    subPages: {
      issuerDetail: {
        component: RemoteIdentityProviderDetail,
        subPages: {
          overview: {},
          clients: {},
          settings: {},
        },
      },
      clientDetail: {
        component: RemoteSessionClientDetail,
        subPages: {
          overview: {},
          mcpServers: {},
          sessions: {},
          settings: {},
        },
      },
    },
  },
  agents: {
    component: AgentManagementRedirect,
  },
  identities: {
    component: IdentitiesRoot,
    indexComponent: PeopleIndexRedirect,
    subPages: {
      people: {
        component: PeopleIndex,
      },
      // indexComponent, not component: a route with children renders its
      // component as their layout, and the roster has no outlet, so it
      // would render in place of every child.
      agents: {
        indexComponent: AgentsIndex,
        subPages: {
          new: {
            component: NewAgentIdentity,
          },
          edit: {
            component: EditAgentIdentity,
          },
        },
      },
      detail: {
        component: IdentityDetailRoot,
        indexComponent: IdentityDetailIndexRedirect,
        subPages: {
          overview: {
            component: IdentityOverview,
          },
          access: {
            component: IdentityAccess,
          },
          usage: {
            component: IdentityUsage,
          },
          security: {
            component: IdentitySecurity,
          },
          findings: {
            component: IdentityFindingsPage,
          },
          cost: {
            component: IdentityCost,
          },
          connections: {
            component: IdentityConnections,
          },
          devices: {
            component: IdentityDevices,
          },
          activity: {
            component: IdentityActivity,
          },
          permissions: {
            component: IdentityAgentSection,
          },
          credentials: {
            component: IdentityAgentSection,
          },
          provisioning: {
            component: IdentityAgentSection,
          },
          sessions: {
            component: IdentityAgentSection,
          },
          controls: {
            component: IdentityAgentSection,
          },
        },
      },
    },
  },
  costs: {
    component: Costs,
    subPages: {
      drill: {
        component: Costs,
      },
    },
  },
  explore: {
    component: Explore,
  },
  dashboards: {
    component: DashboardsRoot,
    indexComponent: DashboardsIndex,
    subPages: {
      builtIn: {
        component: BuiltInDashboardRoute,
      },
      detail: {
        component: DashboardRoute,
      },
    },
  },
  logs: {
    component: LogsRoot,
  },
  agentSessions: {
    component: ChatSessions,
  },
  orgMemory: {
    component: OrgMemory,
  },
  watchdog: {
    component: Watchdog,
  },
  legacyRiskOverview: {
    component: RiskOverviewRedirect,
  },
  detectionRules: {
    component: DetectionRules,
  },
  policyCenter: {
    component: PolicyCenterRoot,
    indexComponent: PolicyCenter,
    subPages: {
      new: {
        component: PolicyNew,
      },
      detail: {
        component: PolicyDetail,
      },
    },
  },
  riskEvents: {
    component: RiskEventsPage,
  },
  shadowAI: {
    component: ShadowAIRoot,
    indexComponent: ShadowAISectionRedirect,
    subPages: {
      harnesses: {
        component: ShadowAIRoot,
        indexComponent: ShadowAISectionRedirect,
        subPages: {
          detail: {
            component: ShadowAIToolDetail,
          },
        },
      },
      assistants: {
        component: ShadowAIRoot,
        indexComponent: ShadowAISectionRedirect,
        subPages: {
          detail: {
            component: ShadowAIToolDetail,
          },
        },
      },
      models: {
        component: ShadowAIRoot,
        indexComponent: ShadowAISectionRedirect,
        subPages: {
          detail: {
            component: ShadowAIToolDetail,
          },
        },
      },
      mcps: {
        component: ShadowAIRoot,
        indexComponent: ShadowMCPIndexRedirect,
        subPages: {
          detail: {
            component: ShadowMCPServerDetail,
          },
        },
      },
    },
  },
  shadowMCP: {
    component: ShadowAIRoot,
    indexComponent: ShadowMCPIndexRedirect,
    subPages: {
      detail: {
        component: ShadowMCPServerLegacyRedirect,
      },
    },
  },
  sdks: {
    component: SDK,
  },
  deployments: {
    component: DeploymentsRoot,
    indexComponent: RedirectToDeployments,
    subPages: {
      deployment: {
        component: Deployment,
      },
    },
  },
  plugins: {
    component: PluginsRoot,
    indexComponent: Plugins,
    subPages: {
      detail: {
        component: PluginDetail,
        subPages: {
          overview: {},
          servers: {},
          skills: {},
          assignments: {},
          settings: {},
        },
      },
    },
  },
  settings: {
    component: Settings,
  },
  docs: {},
};

const ORG_ROUTE_PAGES: PageTree<OrgRouteStructure> = {
  home: {
    component: OrgHome,
  },
  billing: {
    component: Outlet,
    indexComponent: Billing,
    subPages: {
      setup: { component: BillingSetup },
      paymentMethod: { component: BillingPaymentMethod },
    },
  },
  team: {
    component: Team,
  },
  apiKeys: {
    component: OrgApiKeys,
  },
  domains: {
    component: OrgDomains,
  },
  logs: {
    component: OrgLogs,
  },
  gateway: {
    component: OrgGateway,
  },
  legacyData: {
    component: LegacyDataRedirect,
  },
  data: {
    component: EventFeed,
  },
  dataExports: {
    component: DataExports,
  },
  skills: {
    component: OrgSkills,
  },
  platformMcp: {
    component: PlatformMCPRedirect,
  },
  aiIntegrations: {
    component: OrgAIIntegrations,
  },
  webhooks: {
    component: OrgWebhooks,
  },
  userSessionIssuers: {
    component: UserSessionIssuersRoot,
    indexComponent: UserSessionIssuersPage,
    subPages: {
      issuerDetail: {
        component: UserSessionIssuerDetail,
      },
    },
  },
  externalServices: {
    component: ExternalServicesRoot,
    indexComponent: ExternalServicesPage,
    subPages: {
      credentialDetail: {
        component: ExternalCredentialDetail,
        subPages: {
          overview: {},
          kmsKeys: {},
          settings: {},
        },
      },
    },
  },
  encryptionKeys: {
    component: EncryptionKeysRoot,
    indexComponent: EncryptionKeysPage,
    subPages: {
      keyDetail: {
        component: ExternalKeyDetail,
        subPages: {
          overview: {},
          signingKeys: {},
          settings: {},
        },
      },
    },
  },
  signingKeySets: {
    component: SigningKeySetsRoot,
    indexComponent: SigningKeySetsIndex,
    subPages: {
      setDetail: {
        component: JsonWebKeySetDetail,
        subPages: {
          overview: {},
          keys: {},
          settings: {},
        },
      },
    },
  },
  auditLogs: {
    component: OrgAuditLogs,
  },
  killswitch: {
    component: KillswitchesRoot,
    indexComponent: KillswitchIndexRedirect,
    subPages: {
      detail: {
        component: KillswitchRecordRedirect,
      },
    },
  },
  legacyAgents: {
    component: ProjectIdentityRedirect,
  },
  legacyMcpSessions: {
    component: ProjectIdentityRedirect,
  },
  legacyRemoteIdentityProviders: {
    component: ProjectIdentityRedirect,
  },
  identity: {
    component: OrgIdentity,
  },
  platformAdminOverview: {
    component: PlatformAdminOverview,
  },
  platformAdminRbac: {
    component: PlatformAdminRbacOverride,
  },
  platformAdminOnboarding: {
    component: PlatformAdminOnboarding,
  },
  platformAdminOpenRouterKeys: {
    component: PlatformAdminOpenRouterKeys,
  },
  deviceAgent: {
    component: DeviceAgentRoot,
    indexComponent: DeviceAgent,
    subPages: {
      configuration: {
        component: DeviceAgent,
      },
      scanTargets: {
        component: DeviceAgent,
      },
      mdmIntegrations: {
        component: DeviceAgent,
      },
      mdmDetail: {
        component: MdmIntegrationDetail,
      },
    },
  },
  access: {
    component: Access,
    subPages: {
      roles: {
        component: Access,
      },
      members: {
        component: Access,
      },
      assignments: {
        component: Access,
      },
      challenges: {
        component: Access,
      },
    },
  },
  createRole: {
    component: RoleEditorPage,
  },
  editRole: {
    component: RoleEditorPage,
  },
  requestAccess: {
    component: RequestAccess,
  },
  setup: {
    component: SetupWizard,
  },
  setupTask: {
    component: SetupTaskRedirect,
  },
  headless: {
    component: HeadlessMode,
  },
};

const withPages = (
  routes: AppRoutes,
  pages: AnyPageTree,
): Record<string, PagedRoute> =>
  Object.fromEntries(
    Object.entries(pages).map(([key, { subPages, ...routePages }]) => {
      // PageTree has exactly the keys of the route structure.
      const route = routes[key] as AppRoute;
      return [
        key,
        {
          ...route,
          ...routePages,
          // Sub-pages sit on the route object under their own keys.
          ...(subPages
            ? withPages(route as unknown as AppRoutes, subPages)
            : {}),
        },
      ];
    }),
  );

/** The routes nested under a route. Sub-pages sit on it under their own keys. */
export const childRoutes = (route: PagedRoute): PagedRoute[] =>
  Object.values(route).filter(
    (value) =>
      value && typeof value === "object" && "title" in value && "url" in value,
  ) as unknown as PagedRoute[];

/**
 * The href of every route that renders a page, nested routes included. A route
 * behind a path parameter or a splat is skipped along with everything under it:
 * it has no single href to go to.
 */
export const pageHrefs = (
  routes: PagedRoute[],
  hrefs = new Set<string>(),
): Set<string> => {
  for (const route of routes) {
    if (route.external || /[:*]/.test(route.url)) continue;
    // A roster with child routes renders through indexComponent, not
    // component, and is just as reachable.
    if (route.component || route.indexComponent) hrefs.add(route.href());
    pageHrefs(childRoutes(route), hrefs);
  }
  return hrefs;
};

export const withProjectPages = (
  routes: AppRoutes,
): Record<string, PagedRoute> => withPages(routes, ROUTE_PAGES);

export const withOrgPages = (routes: AppRoutes): Record<string, PagedRoute> =>
  withPages(routes, ORG_ROUTE_PAGES);
