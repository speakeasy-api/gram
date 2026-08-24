import { Icon, IconProps } from "@/components/ui/Icon";
import { IconName } from "@/components/ui/Icon/names";
import React, { useMemo } from "react";
import { Link, useLocation, useNavigate } from "react-router";
import { ReleaseStage } from "./components/release-stage-badge";
import { useSlugs } from "./contexts/Sdk";
import { cn } from "./lib/utils";
import AssistantPage from "./pages/assistants/Assistant";
import AssistantsIndex, { AssistantsRoot } from "./pages/assistants/Assistants";
import NewAssistantPage from "./pages/assistants/NewAssistant";
import Billing from "./pages/billing/Billing";
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
import UserSessions from "./pages/org/UserSessions";
import EventFeed from "./pages/data/EventFeed";
import DeviceAgent, { DeviceAgentRoot } from "./pages/device-agent/DeviceAgent";
import MdmIntegrationDetail from "./pages/org/device-integrations/MdmIntegrationDetail";
import Elements from "./pages/elements/Elements";
import EnvironmentPage from "./pages/environments/Environment";
import Environments, {
  EnvironmentsRoot,
} from "./pages/environments/Environments";
import Home from "./pages/home/Home";
import Integrations from "./pages/integrations/Integrations";
import Login from "./pages/login/Login";
import Register from "./pages/login/Register";
import ExploreDemo from "./pages/demo/ExploreDemo";
import SignUp from "./pages/login/SignUp";
import { LogsRoot } from "./pages/logs/Logs";
import { BuiltInMCPDetailPage } from "./pages/mcp/BuiltInMCPDetailPage";
import { MCPDetailPage } from "./pages/mcp/MCPDetails";
import { MCPPage, MCPRoot } from "./pages/mcp/MCP";
import MCPServerDetails from "./pages/mcp/x/MCPServerDetails";
import {
  InsightsEmployeeDetailPage,
  InsightsEmployeesLayout,
  InsightsEmployeesPage,
  InsightsHooksPage,
  InsightsRoot,
} from "./pages/insights/Insights";
import Costs from "./pages/costs/Costs";
import FunctionsOnboarding from "./pages/onboarding/FunctionsOnboarding";
import UploadOpenAPI from "./pages/onboarding/UploadOpenAPI";
import CreateUnproxiedMcp from "./pages/sources/unproxied-mcp/CreateUnproxiedMcp";
import CreateRemoteMcp from "./pages/sources/remote-mcp/CreateRemoteMcp";
import CreateTunneledMcp from "./pages/sources/tunneled-mcp/CreateTunneledMcp";
import { SetupWizard } from "./pages/setup/components/onboarding-wizard";
import Collections, { CollectionsRoot } from "./pages/collections/Collections";
import CollectionDetail from "./pages/collections/CollectionDetail";
import CreateCollection from "./pages/collections/CreateCollection";
import OrgApiKeys from "./pages/org/OrgApiKeys";
import Plugins, { PluginsRoot } from "./pages/plugins/Plugins";
import PluginDetail from "./pages/plugins/PluginDetail";
import OrgAuditLogs from "./pages/org/OrgAuditLogs";
import OrgDomains from "./pages/org/OrgDomains";
import OrgHome from "./pages/org/OrgHome";
import OrgIdentity from "./pages/org/OrgIdentity";
import OrgAIIntegrations from "./pages/org/OrgAIIntegrations";
import OrgLogs from "./pages/org/OrgLogs";
import PlatformMCP from "./pages/org/PlatformMCP";
import OrgSkills from "./pages/org/OrgSkills";
import ExternalCredentialDetail from "./pages/org/external-services/ExternalCredentialDetail";
import {
  ExternalServicesPage,
  ExternalServicesRoot,
} from "./pages/org/external-services/ExternalServices";
import ExternalKeyDetail from "./pages/org/encryption-keys/ExternalKeyDetail";
import {
  EncryptionKeysPage,
  EncryptionKeysRoot,
} from "./pages/org/encryption-keys/EncryptionKeys";
import OrgWebhooks from "./pages/org/OrgWebhooks";
import {
  RemoteIdentityProvidersPage,
  RemoteIdentityProvidersRoot,
} from "./pages/remote-identity-providers/RemoteIdentityProviders";
import RemoteIdentityProviderDetail from "./pages/remote-identity-providers/RemoteIdentityProviderDetail";
import RemoteSessionClientDetail from "./pages/remote-identity-providers/RemoteSessionClientDetail";
import {
  PlatformRemoteIdentityProvidersPage,
  PlatformRemoteIdentityProvidersRoot,
} from "./pages/platform-remote-identity-providers/PlatformRemoteIdentityProviders";
import PlatformRemoteIdentityProviderDetail from "./pages/platform-remote-identity-providers/PlatformRemoteIdentityProviderDetail";
import PlatformAdminOverview from "./pages/platform-admin/Overview";
import PlatformAdminRbacOverride from "./pages/platform-admin/RbacOverride";
import PlatformAdminFeatures from "./pages/platform-admin/Features";
import PlatformAdminOnboarding from "./pages/platform-admin/Onboarding";
import PlatformAdminOpenRouterKeys from "./pages/platform-admin/OpenRouterKeys";
import Playground from "./pages/playground/Playground";
import NewPromptPage from "./pages/prompts/NewPrompt";
import PromptPage from "./pages/prompts/Prompt";
import Prompts, { PromptsRoot } from "./pages/prompts/Prompts";
import SDK from "./pages/sdk/SDK";
import Access from "./pages/access/Access";
import RequestAccess from "./pages/access/RequestAccess";
import Settings from "./pages/settings/Settings";
import TriggersIndex, { TriggersRoot } from "./pages/triggers/Triggers";
import SecurityOverview, {
  RiskOverviewRoot,
} from "./pages/security/SecurityOverview";
import Watchdog from "./pages/security/watchdog/Watchdog";
import RiskEventsPage from "./pages/security/RiskEventsPage";
import ShadowMCP, { ShadowMCPRoot } from "./pages/shadow-mcp/ShadowMCP";
import ShadowMCPServerDetail from "./pages/shadow-mcp/ShadowMCPServerDetail";
import RiskOverviewCategoriesIndex from "./pages/security/RiskOverviewCategoriesIndex";
import RiskOverviewCategoryDetail from "./pages/security/RiskOverviewCategoryDetail";
import RiskOverviewRulesIndex from "./pages/security/RiskOverviewRulesIndex";
import RiskOverviewUserDetail from "./pages/security/RiskOverviewUserDetail";
import RiskOverviewUsersIndex from "./pages/security/RiskOverviewUsersIndex";
import PolicyCenter, { PolicyCenterRoot } from "./pages/security/PolicyCenter";
import PolicyDetail, { PolicyNew } from "./pages/security/PolicyDetail";
import DetectionRules from "./pages/security/DetectionRules";
import Team from "./pages/team/Team";
import SourceDetails from "./pages/sources/SourceDetails";
import {
  AddFromCatalogGate,
  SourcesPage,
  SourcesRoot,
} from "./pages/sources/Sources";
import CustomTools, { CustomToolsRoot } from "./pages/toolBuilder/CustomTools";
import {
  ToolBuilderNew,
  ToolBuilderPage,
} from "./pages/toolBuilder/ToolBuilder";

type AppRouteBasic = {
  title: string;
  url: string;
  external?: boolean;
  icon?: IconName;
  customIcon?: React.ComponentType<{ className?: string }>;
  component?: React.ComponentType;
  indexComponent?: React.ComponentType;
  subPages?: AppRoutesBasic;
  unauthenticated?: boolean;
  outsideMainLayout?: boolean;
  // Release stage badge shown on this route's nav entry. Use sparingly —
  // only for features that are genuinely pre-GA. Page-level badges live on
  // <Page.Section.Title stage="..." /> and must be set separately.
  stage?: ReleaseStage;
};

type GoToFunction = (...params: string[]) => void;

export type AppRoutes = Record<string, AppRoute>;
type AppRoutesBasic = Record<string, AppRouteBasic>;

// App route augmented with some additional utilities
export type AppRoute = Omit<AppRouteBasic, "icon" | "subPages"> & {
  Icon: React.ComponentType<Omit<IconProps, "name">>;
  active: boolean;
  // subPages?: AppRoutes;
  href: (...params: string[]) => string;
  goTo: GoToFunction;
  Link: React.ComponentType<{
    params?: string[];
    queryParams?: Record<string, string>;
    hash?: string;
    className?: string;
    children: React.ReactNode;
  }>;
};

type RouteEntry = {
  title: string;
  url: string;
  icon?: IconName;
  customIcon?: React.ComponentType<{ className?: string }>;
  stage?: ReleaseStage;
} & (
  | {
      external: true;

      component?: never;
      indexComponent?: never;
      unauthenticated?: never;
      subPages?: never;
    }
  | {
      external?: false;

      component?: React.ComponentType;
      indexComponent?: React.ComponentType;
      unauthenticated?: boolean;
      subPages?: Record<string, RouteEntry>;
      outsideMainLayout?: boolean;
    }
);

const ROUTE_STRUCTURE = {
  login: {
    title: "Login",
    url: "/login",
    component: Login,
    unauthenticated: true,
  },
  register: {
    title: "Register",
    url: "/register",
    component: Register,
    unauthenticated: true,
  },
  exploreDemo: {
    title: "Explore demo",
    url: "/explore-demo",
    component: ExploreDemo,
    unauthenticated: true,
  },
  signUp: {
    title: "Sign up",
    url: "/sign-up",
    component: SignUp,
    unauthenticated: true,
  },
  home: {
    // "Home" now belongs to the org-level nav entry; the project's landing
    // page is its overview.
    title: "Project Overview",
    url: "",
    icon: "house",
    component: Home,
  },
  chat: {
    title: "Project Assistant",
    url: "chat",
    icon: "message-circle",
    stage: "beta",
    // Layout route: renders the index (ChatHome) or a conversation subpage.
    component: ChatRoot,
    indexComponent: ChatHome,
    subPages: {
      conversation: {
        title: "Chat",
        url: ":chatId",
        component: ChatConversation,
      },
    },
  },
  playground: {
    title: "Playground",
    url: "playground",
    icon: "message-circle",
    component: Playground,
  },
  elements: {
    title: "Chat Elements",
    url: "elements",
    icon: "message-circle",
    component: Elements,
  },
  integrations: {
    title: "Integrations",
    url: "integrations",
    icon: "package",
    component: Integrations,
  },
  customTools: {
    title: "Custom Tools",
    url: "custom-tools",
    icon: "pencil-ruler",
    component: CustomToolsRoot,
    indexComponent: CustomTools,
    subPages: {
      toolBuilderNew: {
        title: "Tool Builder",
        url: "new",
        component: ToolBuilderNew,
      },
      toolBuilder: {
        title: "Tool Builder",
        url: ":toolName",
        component: ToolBuilderPage,
      },
    },
  },
  prompts: {
    title: "Prompts",
    url: "prompts",
    icon: "newspaper",
    component: PromptsRoot,
    indexComponent: Prompts,
    subPages: {
      newPrompt: {
        title: "New Prompt",
        url: "new",
        component: NewPromptPage,
      },
      prompt: {
        title: "Edit Prompt",
        url: ":promptName",
        component: PromptPage,
      },
    },
  },
  sources: {
    title: "Sources",
    url: "sources",
    icon: "file-code",
    component: SourcesRoot,
    indexComponent: SourcesPage,
    subPages: {
      source: {
        title: "Source Details",
        url: ":sourceKind/:sourceSlug",
        component: SourceDetails,
      },
      addOpenAPI: {
        title: "Add OpenAPI",
        url: "add-openapi",
        component: UploadOpenAPI,
      },
      addFunction: {
        title: "Add Function",
        url: "add-function",
        component: FunctionsOnboarding,
      },
      addFromCatalog: {
        title: "Add from Catalog",
        url: "add-from-catalog",
        component: AddFromCatalogGate,
        indexComponent: Catalog,
      },
      addRemoteMcp: {
        title: "Add Custom Remote MCP Server",
        url: "add-remote-mcp",
        component: CreateRemoteMcp,
      },
      addTunneledMcp: {
        title: "Add Tunneled MCP Server",
        url: "add-tunneled-mcp",
        component: CreateTunneledMcp,
      },
      addUnproxiedMcp: {
        title: "Add Unproxied MCP Server",
        url: "add-unproxied-mcp",
        component: CreateUnproxiedMcp,
      },
    },
  },
  catalog: {
    title: "Catalog",
    url: "catalog",
    icon: "store",
    component: CatalogRoot,
    indexComponent: Catalog,
    subPages: {
      detail: {
        title: "Server Details",
        url: ":serverSpecifier",
        component: CatalogDetailRoot,
        indexComponent: CatalogDetail,
      },
    },
  },
  assistants: {
    title: "Assistants",
    url: "assistants",
    icon: "bot",
    stage: "beta",
    component: AssistantsRoot,
    indexComponent: AssistantsIndex,
    subPages: {
      newAssistant: {
        title: "New Assistant",
        url: "new",
        component: NewAssistantPage,
      },
      detail: {
        title: "Assistant",
        url: ":assistantId",
        component: AssistantPage,
      },
    },
  },
  skills: {
    title: "Skills",
    url: "skills",
    icon: "terminal",
    component: Skills,
    indexComponent: SkillsList,
    subPages: {
      detail: {
        title: "Skill",
        url: ":skillId",
        component: SkillDetailRoot,
        subPages: {
          overview: {
            title: "Skill Overview",
            url: "overview",
            component: SkillOverview,
          },
          content: {
            title: "Skill Content",
            url: "content",
            component: SkillContent,
          },
          usage: {
            title: "Skill Usage",
            url: "usage",
            component: SkillUsage,
          },
          scoredSessions: {
            title: "Scored Sessions",
            url: "scored-sessions",
            component: SkillScoredSessions,
          },
          feedback: {
            title: "Agent Feedback",
            url: "feedback",
            component: SkillFeedback,
          },
          versions: {
            title: "Skill Version History",
            url: "versions",
            component: SkillVersionHistory,
            subPages: {
              version: {
                title: "Skill Version",
                url: ":versionId",
                component: SkillVersionHistory,
              },
            },
          },
          settings: {
            title: "Settings",
            url: "settings",
            component: SkillSettings,
          },
        },
      },
    },
  },
  mcp: {
    title: "MCP",
    url: "mcp",
    icon: "network",
    component: MCPRoot,
    indexComponent: MCPPage,
    subPages: {
      builtIn: {
        title: "Built-in MCP",
        url: "built-in/:builtInSlug",
        component: BuiltInMCPDetailPage,
        subPages: {
          overview: {
            title: "Built-in MCP Overview",
            url: "overview",
          },
          tools: {
            title: "Built-in MCP Tools",
            url: "tools",
          },
        },
      },
      // TODO(AGE-1902): collapse with :toolsetSlug once Hosted (toolset-backed)
      // MCP data moves to mcp_servers/mcp_endpoints. Until then this route is
      // distinct so the new mcp_servers-backed details page renders against
      // mcp_servers without disturbing the existing toolset-backed path. The
      // `x/` prefix is the dashboard's generic experimental namespace; the
      // runtime path for these servers is `/mcp/{slug}` (see AGE-2555).
      x: {
        title: "MCP Server Details",
        url: "x/:mcpServerSlug",
        component: MCPServerDetails,
        subPages: {
          overview: {
            title: "MCP Server Overview",
            url: "overview",
          },
          inspect: {
            title: "MCP Server Inspect",
            url: "inspect",
          },
          // Legacy routes. MCPServerDetails redirects `authentication` to
          // settings#authentication now that authentication lives under
          // Settings, and `tools` to `inspect`.
          authentication: {
            title: "MCP Server Authentication",
            url: "authentication",
          },
          tools: {
            title: "MCP Server Tools",
            url: "tools",
          },
          teamAccess: {
            title: "MCP Server Team Access",
            url: "team-access",
          },
          sessions: {
            title: "MCP Server Clients and Sessions",
            url: "sessions",
          },
          settings: {
            title: "MCP Server Settings",
            url: "settings",
          },
        },
      },
      details: {
        title: "MCP Details",
        url: ":toolsetSlug",
        component: MCPDetailPage,
        subPages: {
          overview: {
            title: "MCP Overview",
            url: "overview",
          },
          tools: {
            title: "MCP Tools",
            url: "tools",
          },
          resources: {
            title: "MCP Resources",
            url: "resources",
          },
          prompts: {
            title: "MCP Prompts",
            url: "prompts",
          },
          authentication: {
            title: "MCP Authentication",
            url: "authentication",
          },
          performance: {
            title: "MCP Performance",
            url: "performance",
          },
          teamAccess: {
            title: "MCP Team Access",
            url: "team-access",
          },
          sessions: {
            title: "MCP Clients and Sessions",
            url: "sessions",
          },
          settings: {
            title: "MCP Settings",
            url: "settings",
          },
        },
      },
    },
  },
  environments: {
    title: "Environments",
    url: "environments",
    icon: "blocks",
    component: EnvironmentsRoot,
    indexComponent: Environments,
    subPages: {
      environment: {
        title: "Environment Details",
        url: ":environmentSlug",
        component: EnvironmentPage,
      },
    },
  },
  triggers: {
    title: "Triggers",
    url: "triggers",
    icon: "zap",
    component: TriggersRoot,
    indexComponent: TriggersIndex,
  },
  insights: {
    title: "MCP & Tools",
    url: "insights",
    icon: "layout-dashboard",
    component: InsightsRoot,
    indexComponent: InsightsHooksPage,
  },
  employees: {
    title: "Employee Enrollment",
    url: "employees",
    icon: "users",
    component: InsightsEmployeesLayout,
    indexComponent: InsightsEmployeesPage,
    subPages: {
      detail: {
        title: "Employee Detail",
        url: ":userSlug",
        component: InsightsEmployeeDetailPage,
      },
    },
  },
  costs: {
    title: "Costs",
    url: "costs",
    icon: "credit-card",
    component: Costs,
    subPages: {
      // Catch-all so the drill path (`/costs/Division~R&D/Department~Eng/…`)
      // renders the same explorer at any depth. CostsExplorer reads the drill
      // levels from the pathname; no per-depth route definition is needed.
      drill: {
        title: "Costs",
        url: "*",
        component: Costs,
      },
    },
  },
  logs: {
    title: "Tool Logs",
    url: "logs",
    icon: "logs",
    component: LogsRoot,
  },
  agentSessions: {
    title: "Agent Sessions",
    url: "agent-sessions",
    icon: "message-square",
    component: ChatSessions,
  },
  orgMemory: {
    title: "Org Memory",
    url: "org-memory",
    icon: "brain",
    stage: "preview",
    component: OrgMemory,
  },
  watchdog: {
    title: "Watchdog",
    url: "watchdog",
    icon: "radar",
    component: Watchdog,
  },
  riskOverview: {
    title: "Risk Overview",
    url: "risk-overview",
    icon: "shield",
    component: RiskOverviewRoot,
    indexComponent: SecurityOverview,
    subPages: {
      usersIndex: {
        title: "Users",
        url: "users",
        component: RiskOverviewUsersIndex,
      },
      userDetail: {
        title: "User",
        url: "users/:externalUserId",
        component: RiskOverviewUserDetail,
      },
      categoriesIndex: {
        title: "Categories",
        url: "categories",
        component: RiskOverviewCategoriesIndex,
      },
      rulesIndex: {
        title: "Rules",
        url: "rules",
        component: RiskOverviewRulesIndex,
      },
      categoryDetail: {
        title: "Category",
        url: "categories/:category",
        component: RiskOverviewCategoryDetail,
      },
    },
  },
  // Legacy URL: Detection Rules lives as a Guardrails tab now; the
  // component redirects there (carrying ?rule= deep links along). Kept out of
  // the sidebar.
  detectionRules: {
    title: "Detection Rules",
    url: "detection-rules",
    icon: "scan-search",
    component: DetectionRules,
  },
  policyCenter: {
    title: "Guardrails",
    url: "risk-policies",
    icon: "shield-check",
    // Layout route: renders the policy list (index) or a policy detail subpage.
    component: PolicyCenterRoot,
    indexComponent: PolicyCenter,
    subPages: {
      new: {
        title: "New policy",
        url: "new",
        component: PolicyNew,
      },
      detail: {
        title: "Policy",
        url: ":policyId",
        component: PolicyDetail,
      },
    },
  },
  riskEvents: {
    title: "Risk Events",
    url: "risk-events",
    icon: "flag",
    component: RiskEventsPage,
  },
  shadowMCP: {
    title: "Shadow MCP",
    url: "shadow-mcp",
    icon: "shield",
    component: ShadowMCPRoot,
    indexComponent: ShadowMCP,
    subPages: {
      detail: {
        title: "Shadow MCP Server",
        url: ":serverSlug",
        component: ShadowMCPServerDetail,
      },
    },
  },
  sdks: {
    title: "SDKs",
    url: "sdks",
    icon: "code",
    component: SDK,
  },
  deployments: {
    title: "Deployments",
    url: "deployments",
    icon: "history",
    component: DeploymentsRoot,
    indexComponent: Deployments,
    subPages: {
      deployment: {
        title: "Overview",
        url: ":deploymentId",
        component: Deployment,
      },
    },
  },
  plugins: {
    title: "Plugins",
    url: "plugins",
    icon: "puzzle",
    component: PluginsRoot,
    indexComponent: Plugins,
    subPages: {
      detail: {
        title: "Plugin",
        url: ":pluginId",
        // PluginDetail renders every section itself, picking the active one
        // from the path — the subpages exist to own the URLs (same shape as
        // mcp.details).
        component: PluginDetail,
        subPages: {
          overview: {
            title: "Plugin Overview",
            url: "overview",
          },
          servers: {
            title: "Plugin MCP Servers",
            url: "servers",
          },
          skills: {
            title: "Plugin Skills",
            url: "skills",
          },
          assignments: {
            title: "Plugin Assignments",
            url: "assignments",
          },
          settings: {
            title: "Plugin Settings",
            url: "settings",
          },
        },
      },
    },
  },
  settings: {
    title: "Project settings",
    url: "settings",
    icon: "settings",
    component: Settings,
  },
  docs: {
    title: "Docs",
    url: "https://docs.getgram.ai",
    icon: "book-open",
    external: true,
  },
} satisfies Record<string, RouteEntry>;

type RouteStructure = typeof ROUTE_STRUCTURE;

/**
 * The point of all this type magic is to make it so you only have to define the routes once
 * and the `useRoutes` hook can add a lot of extra utilities without losing the type safety.
 */

// Transform the AppRouteBasic into an AppRoute, recursing on subPages if present
// so that subPages keeps its route-specific type
type TransformAppRoute<T extends AppRouteBasic> = T extends {
  subPages: AppRoutesBasic;
}
  ? Omit<AppRoute, "subPages"> & TransformRouteToGoTo<T["subPages"]>
  : AppRoute;

type TransformElem<T> = T extends AppRouteBasic
  ? TransformAppRoute<T>
  : T extends AppRouteBasic
    ? TransformRouteToGoTo<T>
    : T;

type TransformRouteToGoTo<T> = {
  [K in keyof T]: TransformElem<T[K]>;
};

type RoutesWithGoTo = TransformRouteToGoTo<RouteStructure>;

export const useRoutes = (overrides?: {
  orgSlug?: string;
  projectSlug?: string;
}): RoutesWithGoTo => {
  const location = useLocation();
  const slugs = useSlugs();
  const orgSlug = overrides?.orgSlug ?? slugs.orgSlug;
  const projectSlug = overrides?.projectSlug ?? slugs.projectSlug;
  const navigate = useNavigate();

  // Check if the current url matches the route url, including dynamic segments
  const matchesCurrent = (url: string) => {
    const urlParts = url.split("/").filter(Boolean);
    const currentParts = location.pathname.split("/").filter(Boolean);

    // Splat routes (trailing `*`, e.g. the costs drill) match any deeper path
    // that shares their prefix — keeps the sidebar item active while drilling.
    if (urlParts[urlParts.length - 1] === "*") {
      const prefix = urlParts.slice(0, -1);
      return (
        currentParts.length >= prefix.length &&
        prefix.every(
          (part, index) => part === currentParts[index] || part.startsWith(":"),
        )
      );
    }

    if (urlParts.length !== currentParts.length) {
      return false;
    }

    return urlParts.every(
      (part, index) => part === currentParts[index] || part.startsWith(":"),
    );
  };

  const addRouteUtilities = (
    route: AppRouteBasic,
    parent?: string,
  ): AppRoute => {
    if (parent === undefined && !route.url.startsWith("/")) {
      parent = `/:orgSlug/projects/:projectSlug`;
    }

    const urlWithParent = `${parent ?? ""}/${route.url}`;

    const resolveUrl = (...params: string[]) => {
      if (route.external) {
        return route.url;
      }

      const parts = urlWithParent.split("/").filter(Boolean);
      const finalParts = [];

      for (const part of parts) {
        if (part.startsWith(":")) {
          if (part === ":orgSlug") {
            finalParts.push(orgSlug);
          } else if (part === ":projectSlug") {
            finalParts.push(projectSlug);
          } else {
            const v = params.shift();
            if (!v) {
              // Instead of throwing an error, fallback to home page
              console.warn(
                `No value provided for ${part}, falling back to home page`,
              );
              return `/${orgSlug}/projects/${projectSlug}`;
            }
            finalParts.push(v);
          }
        } else {
          finalParts.push(part);
        }
      }

      return ("/" + finalParts.join("/")).replace(/\/+/g, "/");
    };

    const goTo = (...params: string[]) => {
      void navigate(resolveUrl(...params));
    };

    const linkComponent = ({
      params = [],
      queryParams = {},
      hash,
      className,
      children,
    }: {
      params?: string[];
      queryParams?: Record<string, string>;
      hash?: string;
      className?: string;
      children: React.ReactNode;
    }) => {
      const queryString = new URLSearchParams(queryParams).toString();
      const hashString = hash ? `#${hash}` : "";
      const queryPart = queryString ? `?${queryString}` : "";
      return (
        <Link
          to={`${resolveUrl(...params)}${queryPart}${hashString}`}
          className={cn("hover:underline", className)}
        >
          {children}
        </Link>
      );
    };

    const subPages = route.subPages
      ? addGoToToRoutes(route.subPages, urlWithParent)
      : undefined;

    const active =
      matchesCurrent(urlWithParent) ||
      !!Object.values(subPages ?? {}).some((subPage) => subPage.active);

    const newRoute: AppRoute = {
      ...route,
      active,
      Icon: route.customIcon
        ? route.customIcon
        : (props: Omit<IconProps, "name">) =>
            route.icon ? <Icon {...props} name={route.icon} /> : null,
      href: resolveUrl,
      goTo,
      Link: linkComponent,
      ...subPages,
    };

    if (route.url.startsWith("/")) {
      newRoute.goTo = () => {
        void route.url;
      };
    }

    return newRoute;
  };

  const addGoToToRoutes = <T extends AppRoutesBasic>(
    routes: T,
    parent?: string,
  ): TransformRouteToGoTo<T> => {
    return Object.fromEntries(
      Object.entries(routes).map(([key, route]) => [
        key,
        addRouteUtilities(route, parent),
      ]),
    ) as TransformRouteToGoTo<T>;
  };

  const routes: RoutesWithGoTo = useMemo(
    () => addGoToToRoutes(ROUTE_STRUCTURE),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- addGoToToRoutes is stable in behavior; recompute only when pathname or slugs change
    [location.pathname, orgSlug, projectSlug, navigate],
  );

  return routes;
};

// --- Org-level routes ---

const ORG_ROUTE_STRUCTURE = {
  home: {
    title: "Home",
    url: "",
    icon: "house",
    component: OrgHome,
  },
  billing: {
    title: "Billing",
    url: "billing",
    icon: "credit-card",
    component: Billing,
  },
  team: {
    title: "Team",
    url: "team",
    icon: "users-round",
    component: Team,
  },
  apiKeys: {
    title: "API Keys",
    url: "api-keys",
    icon: "key-round",
    component: OrgApiKeys,
  },
  domains: {
    title: "Custom Domain",
    url: "domains",
    icon: "globe",
    component: OrgDomains,
  },
  logs: {
    title: "Logging & Telemetry",
    url: "logs",
    icon: "file-text",
    component: OrgLogs,
  },
  data: {
    title: "Event Feed",
    url: "data",
    icon: "activity",
    stage: "preview",
    component: EventFeed,
  },
  skills: {
    title: "Skills",
    url: "skills",
    icon: "terminal",
    component: OrgSkills,
  },
  platformMcp: {
    title: "Platform MCP",
    url: "platform-mcp",
    icon: "plug-zap",
    stage: "preview",
    component: PlatformMCP,
  },
  aiIntegrations: {
    title: "AI Integrations",
    url: "ai-integrations",
    icon: "bot",
    component: OrgAIIntegrations,
  },
  webhooks: {
    title: "Webhooks",
    url: "webhooks",
    icon: "webhook",
    stage: "beta",
    component: OrgWebhooks,
  },
  externalServices: {
    title: "External Services",
    url: "external-services",
    icon: "cloud",
    component: ExternalServicesRoot,
    indexComponent: ExternalServicesPage,
    subPages: {
      // Credentials are namespaced under their own collection segment so that a
      // second kind of external-service resource (encryption keys) can sit
      // beside them rather than having to share this level.
      //
      // The provider segment is part of the resource's own path because the
      // detail page is per-provider: each provider has its own get/update
      // endpoints and its own fields, so a deep link has to carry which one it
      // is rather than relying on state handed over from the list.
      credentialDetail: {
        title: "External Credential",
        url: "credentials/:provider/:credentialId",
        component: ExternalCredentialDetail,
        subPages: {
          overview: { title: "Overview", url: "overview" },
          kmsKeys: { title: "KMS Keys", url: "kms-keys" },
          settings: { title: "Settings", url: "settings" },
        },
      },
    },
  },
  encryptionKeys: {
    title: "Encryption Keys",
    url: "encryption-keys",
    icon: "key-square",
    component: EncryptionKeysRoot,
    indexComponent: EncryptionKeysPage,
    subPages: {
      // Keyed on the provider for the same reason credentials are: the detail
      // page is per-provider, with its own get/update endpoints and its own
      // fields, so a deep link has to carry which provider it names rather than
      // relying on state handed over from the list.
      keyDetail: {
        title: "Encryption Key",
        url: ":provider/:keyId",
        component: ExternalKeyDetail,
        subPages: {
          overview: { title: "Overview", url: "overview" },
          settings: { title: "Settings", url: "settings" },
        },
      },
    },
  },
  auditLogs: {
    title: "Audit Logs",
    url: "audit-logs",
    icon: "history",
    component: OrgAuditLogs,
  },
  mcpSessions: {
    title: "MCP Sessions",
    url: "mcp-sessions",
    icon: "users",
    component: UserSessions,
  },
  identity: {
    title: "IDP and SSO",
    url: "identity",
    icon: "fingerprint",
    component: OrgIdentity,
  },
  remoteIdentityProviders: {
    title: "Remote Identity Providers",
    url: "remote-identity-providers",
    icon: "key-round",
    component: RemoteIdentityProvidersRoot,
    indexComponent: RemoteIdentityProvidersPage,
    subPages: {
      issuerDetail: {
        title: "Remote Identity Provider",
        url: ":issuerId",
        component: RemoteIdentityProviderDetail,
        subPages: {
          overview: { title: "Overview", url: "overview" },
          clients: { title: "Clients", url: "clients" },
          settings: { title: "Settings", url: "settings" },
        },
      },
      clientDetail: {
        title: "Remote Session Client",
        url: ":issuerId/clients/:clientId",
        component: RemoteSessionClientDetail,
        subPages: {
          overview: { title: "Overview", url: "overview" },
          mcpServers: { title: "MCP Servers", url: "mcp-servers" },
          sessions: { title: "Sessions", url: "sessions" },
          settings: { title: "Settings", url: "settings" },
        },
      },
    },
  },
  // The platform catalog gets its own base path rather than a static segment
  // under remote-identity-providers, where it would be a sibling of the
  // `:issuerId` route and rely on the router ranking static above dynamic to
  // not be swallowed by it. Platform-admin only; see PlatformAdminOnly.
  platformRemoteIdentityProviders: {
    // Kept distinct from the tenant route's title: nav items register by title
    // (see CollapsibleNavItem), and Recents and the command palette show it
    // without a group header to disambiguate. The sidebar renders the shorter
    // "Remote Identity Providers" under the Platform Admin header, and this
    // also matches the URL-derived breadcrumb.
    title: "Platform Remote Identity Providers",
    url: "platform-remote-identity-providers",
    icon: "key-round",
    component: PlatformRemoteIdentityProvidersRoot,
    indexComponent: PlatformRemoteIdentityProvidersPage,
    subPages: {
      issuerDetail: {
        title: "Platform Remote Identity Provider",
        url: ":issuerId",
        component: PlatformRemoteIdentityProviderDetail,
        subPages: {
          overview: { title: "Overview", url: "overview" },
          convergence: { title: "Convergence", url: "convergence" },
          settings: { title: "Settings", url: "settings" },
        },
      },
    },
  },
  // Platform Admin pages — the former floating Developer Toolkit, one page per
  // old tab. Speakeasy staff only (plus local dev); see PlatformAdminGate.
  platformAdminOverview: {
    title: "Platform Admin Overview",
    url: "platform-admin",
    icon: "crown",
    component: PlatformAdminOverview,
  },
  platformAdminRbac: {
    title: "RBAC Override",
    url: "platform-admin/rbac",
    icon: "shield",
    component: PlatformAdminRbacOverride,
  },
  platformAdminFeatures: {
    title: "Platform Features",
    url: "platform-admin/features",
    icon: "sliders-horizontal",
    component: PlatformAdminFeatures,
  },
  platformAdminOnboarding: {
    title: "Enterprise Onboarding",
    url: "platform-admin/onboarding",
    icon: "mail",
    component: PlatformAdminOnboarding,
  },
  platformAdminOpenRouterKeys: {
    title: "OpenRouter Keys",
    url: "platform-admin/openrouter-keys",
    icon: "key-round",
    component: PlatformAdminOpenRouterKeys,
  },
  deviceAgent: {
    title: "Device Agent",
    url: "device-agent",
    icon: "laptop",
    component: DeviceAgentRoot,
    indexComponent: DeviceAgent,
    subPages: {
      configuration: {
        title: "Configuration",
        url: "configuration",
        component: DeviceAgent,
      },
      mdmIntegrations: {
        title: "MDM Integrations",
        url: "mdm-integrations",
        component: DeviceAgent,
      },
      mdmDetail: {
        title: "MDM Integration",
        url: "mdm-integrations/:provider",
        component: MdmIntegrationDetail,
      },
    },
  },
  access: {
    title: "Roles & Permissions",
    url: "access",
    icon: "shield",
    component: Access,
    subPages: {
      roles: {
        title: "Roles & Permissions",
        url: "roles",
        component: Access,
      },
      members: {
        title: "Roles & Permissions",
        url: "members",
        component: Access,
      },
      challenges: {
        title: "Roles & Permissions",
        url: "challenges",
        component: Access,
      },
    },
  },
  requestAccess: {
    title: "Request Access",
    url: "request-access",
    component: RequestAccess,
    outsideMainLayout: true,
  },
  collections: {
    title: "Collections",
    url: "collections",
    icon: "layout-grid",
    component: CollectionsRoot,
    indexComponent: Collections,
    subPages: {
      create: {
        title: "Create Collection",
        url: "create",
        component: CreateCollection,
      },
      detail: {
        title: "Collection",
        url: ":collectionSlug",
        component: CollectionDetail,
      },
    },
  },
  setup: {
    title: "Setup",
    url: "setup",
    icon: "settings",
    component: SetupWizard,
    outsideMainLayout: true,
  },
} satisfies Record<string, RouteEntry>;

type OrgRouteStructure = typeof ORG_ROUTE_STRUCTURE;
type OrgRoutesWithGoTo = TransformRouteToGoTo<OrgRouteStructure>;

/** The URL segments used by org-level routes (for redirect logic). */
export const orgRoutePaths = Object.values(ORG_ROUTE_STRUCTURE)
  .map((r) => r.url)
  .filter(Boolean);

export const useOrgRoutes = (): OrgRoutesWithGoTo => {
  const location = useLocation();
  const { orgSlug } = useSlugs();
  const navigate = useNavigate();

  const matchesCurrent = (url: string) => {
    const urlParts = url.split("/").filter(Boolean);
    const currentParts = location.pathname.split("/").filter(Boolean);

    if (urlParts.length !== currentParts.length) {
      return false;
    }

    return urlParts.every(
      (part, index) => part === currentParts[index] || part.startsWith(":"),
    );
  };

  const addRouteUtilities = (
    route: AppRouteBasic,
    parent?: string,
  ): AppRoute => {
    if (parent === undefined && !route.url.startsWith("/")) {
      parent = `/:orgSlug`;
    }

    const urlWithParent = `${parent ?? ""}/${route.url}`;

    const resolveUrl = (...params: string[]) => {
      if (route.external) {
        return route.url;
      }

      const parts = urlWithParent.split("/").filter(Boolean);
      const finalParts = [];

      for (const part of parts) {
        if (part.startsWith(":")) {
          if (part === ":orgSlug") {
            finalParts.push(orgSlug);
          } else {
            const v = params.shift();
            if (!v) {
              console.warn(
                `No value provided for ${part}, falling back to org home`,
              );
              return `/${orgSlug}`;
            }
            finalParts.push(v);
          }
        } else {
          finalParts.push(part);
        }
      }

      return ("/" + finalParts.join("/")).replace(/\/+/g, "/");
    };

    const goTo = (...params: string[]) => {
      void navigate(resolveUrl(...params));
    };

    const linkComponent = ({
      params = [],
      queryParams = {},
      hash,
      className,
      children,
    }: {
      params?: string[];
      queryParams?: Record<string, string>;
      hash?: string;
      className?: string;
      children: React.ReactNode;
    }) => {
      const queryString = new URLSearchParams(queryParams).toString();
      const hashString = hash ? `#${hash}` : "";
      const queryPart = queryString ? `?${queryString}` : "";
      return (
        <Link
          to={`${resolveUrl(...params)}${queryPart}${hashString}`}
          className={cn("hover:underline", className)}
        >
          {children}
        </Link>
      );
    };

    const subPages = route.subPages
      ? addGoToToRoutes(route.subPages, urlWithParent)
      : undefined;

    const active =
      matchesCurrent(urlWithParent) ||
      !!Object.values(subPages ?? {}).some((subPage) => subPage.active);

    const newRoute: AppRoute = {
      ...route,
      active,
      Icon: route.customIcon
        ? route.customIcon
        : (props: Omit<IconProps, "name">) =>
            route.icon ? <Icon {...props} name={route.icon} /> : null,
      href: resolveUrl,
      goTo,
      Link: linkComponent,
      ...subPages,
    };

    return newRoute;
  };

  const addGoToToRoutes = <T extends AppRoutesBasic>(
    routes: T,
    parent?: string,
  ): TransformRouteToGoTo<T> => {
    return Object.fromEntries(
      Object.entries(routes).map(([key, route]) => [
        key,
        addRouteUtilities(route, parent),
      ]),
    ) as TransformRouteToGoTo<T>;
  };

  const routes: OrgRoutesWithGoTo = useMemo(
    () => addGoToToRoutes(ORG_ROUTE_STRUCTURE),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- addGoToToRoutes is stable in behavior; recompute only when pathname or slug change
    [location.pathname, orgSlug, navigate],
  );

  return routes;
};
