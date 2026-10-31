import { Icon, IconProps } from "@/components/ui/Icon";
import { IconName } from "@/components/ui/Icon/names";
import React, { useMemo } from "react";
import { Link, useLocation, useNavigate } from "react-router";
import { ReleaseStage } from "./components/release-stage-badge";
import { useSlugs } from "./contexts/Sdk";
import { cn } from "./lib/utils";

type AppRouteBasic = {
  title: string;
  url: string;
  external?: boolean;
  icon?: IconName;
  customIcon?: React.ComponentType<{ className?: string }>;
  subPages?: AppRoutesBasic;
  unauthenticated?: boolean;
  outsideMainLayout?: boolean;
  // This route only exists to resolve an old URL: its component redirects
  // elsewhere. Surfaces that enumerate routes as destinations (the command
  // palette) skip it, so a bookmark keeps working without the dead entry
  // being offered as somewhere to go.
  legacyRedirect?: boolean;
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

      unauthenticated?: never;
      subPages?: never;
    }
  | {
      external?: false;

      unauthenticated?: boolean;
      subPages?: Record<string, RouteEntry>;
      outsideMainLayout?: boolean;
      legacyRedirect?: boolean;
    }
);

const ROUTE_STRUCTURE = {
  login: {
    title: "Login",
    url: "/login",
    unauthenticated: true,
  },
  register: {
    title: "Register",
    url: "/register",
    unauthenticated: true,
  },
  exploreDemo: {
    title: "Explore demo",
    url: "/explore-demo",
    unauthenticated: true,
  },
  signUp: {
    title: "Sign up",
    url: "/sign-up",
    unauthenticated: true,
  },
  home: {
    // "Home" now belongs to the org-level nav entry; the project's landing
    // page is its overview.
    title: "Project Overview",
    url: "",
    icon: "house",
  },
  guide: {
    title: "Project Guide",
    url: "guide",
  },
  chat: {
    title: "Project Assistant",
    url: "chat",
    icon: "message-circle",
    stage: "beta",
    // Layout route: renders the index (ChatHome) or a conversation subpage.
    subPages: {
      conversation: {
        title: "Chat",
        url: ":chatId",
      },
    },
  },
  playground: {
    title: "Playground",
    url: "playground",
    icon: "message-circle",
  },
  integrations: {
    title: "Integrations",
    url: "integrations",
    icon: "package",
  },
  prompts: {
    title: "Prompts",
    url: "prompts",
    icon: "newspaper",
    subPages: {
      newPrompt: {
        title: "New Prompt",
        url: "new",
      },
      prompt: {
        title: "Edit Prompt",
        url: ":promptName",
      },
    },
  },
  assistants: {
    title: "Assistants",
    url: "assistants",
    icon: "bot",
    stage: "beta",
    subPages: {
      newAssistant: {
        title: "New Assistant",
        url: "new",
      },
      detail: {
        title: "Assistant",
        url: ":assistantId",
      },
    },
  },
  signalsIntelligence: {
    title: "Signals intelligence",
    url: "signals-intelligence",
    icon: "radio",
    stage: "preview",
  },
  skills: {
    title: "Skills",
    url: "skills",
    icon: "terminal",
    subPages: {
      detail: {
        title: "Skill",
        url: ":skillId",
        subPages: {
          overview: {
            title: "Skill Overview",
            url: "overview",
          },
          content: {
            title: "Skill Content",
            url: "content",
          },
          usage: {
            title: "Skill Usage",
            url: "usage",
          },
          scoredSessions: {
            title: "Scored Sessions",
            url: "scored-sessions",
          },
          feedback: {
            title: "Agent Feedback",
            url: "feedback",
          },
          versions: {
            title: "Skill Version History",
            url: "versions",
            subPages: {
              version: {
                title: "Skill Version",
                url: ":versionId",
              },
            },
          },
          settings: {
            title: "Settings",
            url: "settings",
          },
        },
      },
    },
  },
  mcp: {
    title: "MCP",
    url: "mcp",
    icon: "network",
    subPages: {
      // S-853: MCP is the inventory, so every way of adding a server starts
      // here. Options are named for how the server is reached rather than for
      // the backend that stores it, and OpenAPI and functions sit under
      // "advanced" on the same page instead of in the navigation.
      //
      // `add`, `catalog`, `sources` and the other static segments under `mcp`
      // rank above the `details` `:mcpServerSlug` route. Links to a server
      // whose slug equals one of them use its id instead (mcpServerRouteParam).
      add: {
        title: "Add MCP Server",
        url: "add",
        subPages: {
          remote: {
            title: "Add Remote MCP Server",
            url: "remote",
          },
          tunneled: {
            title: "Add Tunneled MCP Server",
            url: "tunneled",
          },
          fromSource: {
            title: "From Existing Source",
            url: "from-existing-source",
          },
          openapi: {
            title: "Add OpenAPI",
            url: "openapi",
          },
          function: {
            title: "Add Function",
            url: "function",
          },
        },
      },
      // The single catalog entry point, and a tab of the MCP index rather than
      // a step inside the add flow: it is the one surface people go looking for
      // by name. `/catalog`, `/sources/add-from-catalog` and the earlier
      // `/mcp/add/catalog` all redirect here.
      catalog: {
        title: "Catalog",
        url: "catalog",
        subPages: {
          detail: {
            title: "Server Details",
            url: ":serverSpecifier",
          },
        },
      },
      // Sources have no section of their own any more, but the CLI links here
      // after a push and functions still arrive this way, so they keep a page.
      deployments: {
        title: "Deployments",
        url: "deployments",
      },
      sources: {
        title: "Sources",
        url: "sources",
        subPages: {
          detail: {
            title: "Source",
            url: ":sourceId",
          },
        },
      },
      // The Organization Gateway. A static segment beside `add`,
      // `catalog` and `sources`, with the same latent toolset-slug collision.
      // It lives under MCP rather than the organization settings because it
      // is opened from the head of the MCP Servers list.
      organizationGateway: {
        title: "Organization Gateway",
        url: "organization-gateway",
      },
      // Gateway Endpoints (meta MCP servers). Addressed by id: a gateway has
      // no slug of its own — routing identity lives on its mcp_endpoints.
      gateway: {
        title: "Gateway Details",
        url: "gateway/:gatewayId",
        subPages: {
          overview: {
            title: "Gateway Overview",
            url: "overview",
          },
          // Legacy URL: member management moved onto the Overview tab, and
          // GatewayDetails redirects unknown tab segments there. Kept so old
          // links keep resolving to the gateway page.
          members: {
            title: "Gateway Members",
            url: "members",
          },
          inspect: {
            title: "Gateway Inspect",
            url: "inspect",
          },
          skills: {
            title: "Gateway Skills",
            url: "skills",
          },
          teamAccess: {
            title: "Gateway Team Access",
            url: "team-access",
          },
          sessions: {
            title: "Gateway Clients and Sessions",
            url: "sessions",
          },
          settings: {
            title: "Gateway Settings",
            url: "settings",
          },
        },
      },
      // Every MCP server's page, by slug or id; ToolsetRedirectPage resolves misses.
      details: {
        title: "MCP Server Details",
        url: ":mcpServerSlug",
        subPages: {
          overview: {
            title: "MCP Server Overview",
            url: "overview",
          },
          inspect: {
            title: "MCP Server Inspect",
            url: "inspect",
          },
          // Hosted servers' tools; on other servers this redirects to Inspect.
          tools: {
            title: "MCP Server Tools",
            url: "tools",
          },
          resources: {
            title: "MCP Server Resources",
            url: "resources",
          },
          prompts: {
            title: "MCP Server Prompts",
            url: "prompts",
          },
          skills: {
            title: "MCP Server Skills",
            url: "skills",
          },
          teamAccess: {
            title: "MCP Server Team Access",
            url: "team-access",
          },
          guardrails: {
            title: "MCP Server Guardrails",
            url: "guardrails",
          },
          sessions: {
            title: "MCP Server Clients and Sessions",
            url: "sessions",
          },
          settings: {
            title: "MCP Server Settings",
            url: "settings",
          },
          // Retired tabs: redirect to settings#authentication / #performance.
          authentication: {
            title: "MCP Server Authentication",
            url: "authentication",
            legacyRedirect: true,
          },
          performance: {
            title: "MCP Server Performance",
            url: "performance",
            legacyRedirect: true,
          },
        },
      },
    },
  },
  // In-app links that know a toolset, not its server; redirects to the hosting server.
  toolset: {
    title: "Toolset",
    url: "toolsets/:toolsetSlug",
    legacyRedirect: true,
    subPages: {
      overview: { title: "Toolset Overview", url: "overview" },
      tools: { title: "Toolset Tools", url: "tools" },
      resources: { title: "Toolset Resources", url: "resources" },
      prompts: { title: "Toolset Prompts", url: "prompts" },
      inspect: { title: "Toolset Inspect", url: "inspect" },
      teamAccess: { title: "Toolset Team Access", url: "team-access" },
      guardrails: { title: "Toolset Guardrails", url: "guardrails" },
      sessions: { title: "Toolset Sessions", url: "sessions" },
      settings: { title: "Toolset Settings", url: "settings" },
      authentication: {
        title: "Toolset Authentication",
        url: "authentication",
      },
      performance: { title: "Toolset Performance", url: "performance" },
    },
  },
  environments: {
    title: "Environments",
    url: "environments",
    icon: "blocks",
    subPages: {
      environment: {
        title: "Environment Details",
        url: ":environmentSlug",
      },
    },
  },
  triggers: {
    title: "Triggers",
    url: "triggers",
    icon: "zap",
  },
  insights: {
    title: "MCP & Tools",
    url: "insights",
    icon: "layout-dashboard",
  },
  mcpSessions: {
    title: "MCP Sessions",
    url: "mcp-sessions",
    icon: "users",
  },

  remoteIdentityProviders: {
    title: "Authorization Servers",
    url: "remote-identity-providers",
    icon: "key-round",
    subPages: {
      issuerDetail: {
        title: "Authorization Server",
        url: ":issuerId",
        subPages: {
          overview: { title: "Overview", url: "overview" },
          clients: { title: "Clients", url: "clients" },
          settings: { title: "Settings", url: "settings" },
        },
      },
      clientDetail: {
        title: "Remote Session Client",
        url: ":issuerId/clients/:clientId",
        subPages: {
          overview: { title: "Overview", url: "overview" },
          mcpServers: { title: "MCP Servers", url: "mcp-servers" },
          sessions: { title: "Sessions", url: "sessions" },
          // Merged into Overview; RemoteSessionClientDetail redirects it there.
          settings: {
            title: "Settings",
            url: "settings",
            legacyRedirect: true,
          },
        },
      },
    },
  },

  // Kept as a redirect only: agents live under Identities now. Links minted
  // before that, and anything a reader bookmarked, still resolve.
  agents: {
    title: "Agent Identity",
    url: "agent-management",
    icon: "bot",
    legacyRedirect: true,
  },
  // One page per person, reached from every surface that renders a human. The
  // URL segment is an identity URN (`user:...`, `email:...`, `external:...`),
  // url-encoded; the resolver folds every identifier for a subject onto the
  // same canonical URN, so links built from different systems converge here.
  //
  // Project-level, where Employee Enrollment sat: a project is how an
  // organization segments the people it manages, and usage, cost, chats and
  // risk are all recorded per project. The org-scoped reads behind the page —
  // identity resolution, the directory, roles, devices, the audit trail and the
  // challenge log — answer the same whichever project you arrive from.
  identities: {
    title: "Identities",
    url: "identities",
    icon: "users",
    // The bare URL holds no roster of its own: people and agents are two
    // rosters, and this sends you to the people.
    subPages: {
      people: {
        title: "People",
        url: "people",
        icon: "users",
      },
      agents: {
        title: "Agents",
        url: "agents",
        icon: "bot",
        stage: "preview",
        subPages: {
          new: {
            title: "New Agent Identity",
            url: "new",
          },
          edit: {
            title: "Edit Agent Identity",
            url: ":agentId/edit",
          },
        },
      },
      detail: {
        title: "Identity",
        url: ":identityUrn",
        subPages: {
          overview: {
            title: "Identity Overview",
            url: "overview",
          },
          access: {
            title: "Identity Access",
            url: "access",
          },
          usage: {
            title: "Identity Usage",
            url: "usage",
          },
          security: {
            title: "Identity Security",
            url: "security",
          },
          findings: {
            title: "Identity Findings",
            url: "findings",
          },
          cost: {
            title: "Identity Cost",
            url: "cost",
          },
          connections: {
            title: "Identity Connections",
            url: "connections",
          },
          devices: {
            title: "Identity Devices",
            url: "devices",
          },
          activity: {
            title: "Identity Activity",
            url: "activity",
          },
          // An agent is a different subject from a person, and its sections
          // are named for what an agent holds. They are their own segments so
          // the address, the breadcrumb and the tab agree: an agent's keys
          // served at /devices read as "Devices" in the crumb bar whatever
          // the tab was labelled. The person segments above are unchanged.
          permissions: {
            title: "Agent Permissions",
            url: "permissions",
          },
          credentials: {
            title: "Agent Credentials",
            url: "credentials",
          },
          // The section this one was renamed from. Kept so the links and
          // bookmarks that name it still resolve: the page itself sends the
          // old segment on to the new one.
          provisioning: {
            title: "Agent Credentials",
            url: "provisioning",
          },
          sessions: {
            title: "Agent Sessions",
            url: "sessions",
          },
          controls: {
            title: "Agent Controls",
            url: "controls",
          },
        },
      },
    },
  },
  costs: {
    title: "Costs",
    url: "costs",
    icon: "credit-card",
    subPages: {
      // Catch-all so the drill path (`/costs/Division~R&D/Department~Eng/…`)
      // renders the same explorer at any depth. CostsExplorer reads the drill
      // levels from the pathname; no per-depth route definition is needed.
      drill: {
        title: "Costs",
        url: "*",
      },
    },
  },
  explore: {
    title: "Explore",
    url: "explore",
    icon: "telescope",
    stage: "preview",
  },
  dashboards: {
    title: "Dashboards",
    url: "dashboards",
    icon: "layout-grid",
    stage: "preview",
    subPages: {
      builtIn: {
        title: "Dashboard",
        url: "builtin/:slug",
      },
      detail: {
        title: "Dashboard",
        url: ":dashboardId",
      },
    },
  },
  logs: {
    title: "Tool Logs",
    url: "logs",
    icon: "logs",
  },
  agentSessions: {
    title: "Agent Sessions",
    url: "agent-sessions",
    icon: "message-square",
  },
  orgMemory: {
    title: "Org Memory",
    url: "org-memory",
    icon: "brain",
    stage: "preview",
  },
  watchdog: {
    title: "Watchdog",
    url: "watchdog",
    icon: "radar",
  },
  // Retired page: old links and bookmarks, including its drill-downs, land on
  // Watchdog.
  legacyRiskOverview: {
    title: "Risk Overview",
    url: "risk-overview/*",
    legacyRedirect: true,
  },
  // Legacy URL: Detection Rules lives as a Guardrails tab now; the
  // component redirects there (carrying ?rule= deep links along). Kept out of
  // the sidebar.
  detectionRules: {
    title: "Detection Rules",
    url: "detection-rules",
    icon: "scan-search",
  },
  policyCenter: {
    title: "Guardrails",
    url: "risk-policies",
    icon: "shield-check",
    // Layout route: renders the policy list (index) or a policy detail subpage.
    subPages: {
      new: {
        title: "New policy",
        url: "new",
      },
      detail: {
        title: "Policy",
        url: ":policyId",
      },
    },
  },
  riskEvents: {
    title: "Risk Events",
    url: "risk-events",
    icon: "flag",
  },
  // Shadow AI lives in Guardrails (the Shadow AI policy and its targets tab)
  // and is not in the nav. Its section URLs stay because tickets, bookmarks
  // and block messages link to them: every index page redirects to
  // Guardrails, and the tool and server detail pages beneath keep rendering.
  //
  // Project-level placement with organization-scoped reads behind it, the
  // same shape as identities above and for the same reason.
  shadowAI: {
    title: "Shadow AI",
    url: "shadow-ai",
    icon: "shield",
    subPages: {
      // A tool's detail is nested under its category, as the server detail
      // is under MCPs, so the breadcrumb says which kind a pasted link is.
      harnesses: {
        title: "Harnesses",
        url: "harnesses",
        subPages: {
          detail: {
            title: "Shadow AI Tool",
            url: ":targetId",
          },
        },
      },
      assistants: {
        title: "Assistants",
        url: "assistants",
        subPages: {
          detail: {
            title: "Shadow AI Tool",
            url: ":targetId",
          },
        },
      },
      models: {
        title: "Models",
        url: "models",
        subPages: {
          detail: {
            title: "Shadow AI Tool",
            url: ":targetId",
          },
        },
      },
      mcps: {
        title: "MCPs",
        url: "mcps",
        // The MCPs index was the approvals queue; it lands on the MCP
        // approvals tab of Guardrails rather than the policies list.
        subPages: {
          detail: {
            title: "Shadow MCP Server",
            url: ":serverSlug",
          },
        },
      },
    },
  },
  // The Shadow MCP paths predate the section and are sitting in bookmarks and
  // tickets, so they redirect permanently rather than 404. The end-user
  // request URLs (/shadow-mcp/request, /risk-policy-bypass/request) are
  // top-level routes in App.tsx, are quoted verbatim in block messages
  // already in people's terminals, and are deliberately left alone.
  shadowMCP: {
    title: "Shadow MCP",
    url: "shadow-mcp",
    subPages: {
      detail: {
        title: "Shadow MCP Server",
        url: ":serverSlug",
      },
    },
  },
  sdks: {
    title: "SDKs",
    url: "sdks",
    icon: "code",
  },
  deployments: {
    title: "Deployments",
    url: "deployments",
    icon: "history",
    // The listing lives on the MCP page as a tab now; this index only
    // forwards. The detail route below stays put — it is the URL the CLI
    // prints after a push.
    subPages: {
      deployment: {
        title: "Overview",
        url: ":deploymentId",
      },
    },
  },
  plugins: {
    title: "Plugins",
    url: "plugins",
    icon: "puzzle",
    subPages: {
      detail: {
        title: "Plugin",
        url: ":pluginId",
        // PluginDetail renders every section itself, picking the active one
        // from the path — the subpages exist to own the URLs (same shape as
        // mcp.details).
        subPages: {
          overview: {
            title: "Plugin Overview",
            url: "overview",
          },
          // Keep existing server links working after merging servers into overview.
          servers: {
            title: "Plugin Overview",
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
  },
  docs: {
    title: "Docs",
    url: "https://docs.getgram.ai",
    icon: "book-open",
    external: true,
  },
} satisfies Record<string, RouteEntry>;

export type RouteStructure = typeof ROUTE_STRUCTURE;

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
    staticSiblings?: Set<string>,
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

    // A dynamic route must not claim the current URL when a static sibling
    // owns it. `/mcp/:mcpServerSlug` and `/mcp/add` have the same segment count
    // and matchesCurrent treats `:param` as a wildcard, so without this the
    // detail route reads as active on `/mcp/add` — which is what selects the
    // sidebar variant. The router already ranks static above dynamic; this
    // makes `active` agree with where the user actually landed.
    const shadowedByStaticSibling =
      route.url.startsWith(":") &&
      !!staticSiblings?.has(
        location.pathname.split("/").filter(Boolean)[
          (parent ?? "").split("/").filter(Boolean).length
        ] ?? "",
      );

    // A shadowed route stays inactive even when a sub-page path matches:
    // `/mcp/catalog/overview` is a catalog entry, not a server's Overview.
    const active =
      !shadowedByStaticSibling &&
      (matchesCurrent(urlWithParent) ||
        !!Object.values(subPages ?? {}).some((subPage) => subPage.active));

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
    const staticSiblings = new Set(
      Object.values(routes)
        .map((route) => route.url.split("/")[0] ?? "")
        .filter((segment) => segment !== "" && !segment.startsWith(":")),
    );
    return Object.fromEntries(
      Object.entries(routes).map(([key, route]) => [
        key,
        addRouteUtilities(route, parent, staticSiblings),
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
  },
  billing: {
    title: "Billing",
    url: "billing",
    icon: "credit-card",
    subPages: {
      setup: { title: "Billing setup", url: "setup" },
      paymentMethod: { title: "Payment method", url: "payment-method" },
    },
  },
  team: {
    title: "Team",
    url: "team",
    icon: "users-round",
  },
  apiKeys: {
    title: "API Keys",
    url: "api-keys",
    icon: "key-round",
  },
  domains: {
    title: "Custom Domain",
    url: "domains",
    icon: "globe",
  },
  logs: {
    title: "Logging & Telemetry",
    url: "logs",
    icon: "file-text",
  },
  gateway: {
    title: "Org Gateway",
    url: "gateway",
    icon: "network",
  },
  legacyData: {
    title: "Event Feed",
    url: "data",
    icon: "activity",
  },
  data: {
    title: "Event Feed",
    url: "data/event-feed",
    icon: "activity",
    stage: "preview",
  },
  dataExports: {
    title: "Exports",
    url: "data/exports",
    icon: "send",
  },
  skills: {
    title: "Skills",
    url: "skills",
    icon: "terminal",
  },
  // Legacy URL: Platform MCP setup is what headless mode does now, so the old
  // standalone page redirects there. Kept out of the sidebar, and flagged so
  // the command palette does not offer it as a destination either.
  platformMcp: {
    title: "Platform MCP",
    url: "platform-mcp",
    legacyRedirect: true,
  },
  aiIntegrations: {
    title: "AI Integrations",
    url: "ai-integrations",
    icon: "bot",
  },
  webhooks: {
    title: "Webhooks",
    url: "webhooks",
    icon: "webhook",
    stage: "beta",
  },
  // Rolled out behind gram-user-session-issuer-dashboard, which the root
  // checks; the sidebar hides the entry while it is off.
  userSessionIssuers: {
    title: "Speakeasy Authorization Servers",
    url: "user-session-issuers",
    icon: "key-round",
    subPages: {
      issuerDetail: {
        title: "Speakeasy Authorization Server",
        url: ":issuerId",
      },
    },
  },
  externalServices: {
    title: "External Services",
    url: "external-services",
    icon: "cloud",
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
    subPages: {
      // Keyed on the provider for the same reason credentials are: the detail
      // page is per-provider, with its own get/update endpoints and its own
      // fields, so a deep link has to carry which provider it names rather than
      // relying on state handed over from the list.
      keyDetail: {
        title: "Encryption Key",
        url: ":provider/:keyId",
        subPages: {
          overview: { title: "Overview", url: "overview" },
          signingKeys: { title: "Signing Keys", url: "signing-keys" },
          settings: { title: "Settings", url: "settings" },
        },
      },
    },
  },
  // Signing key sets are listed on the Encryption Keys page (a set is the
  // published face of the KMS key that backs it), but their detail pages get
  // their own top-level path so a set id can never be mistaken for the
  // :provider/:keyId segments of an encryption key. The index redirects to
  // that list, which keeps the breadcrumb and command palette entry landing
  // somewhere real. Not in the sidebar.
  signingKeySets: {
    title: "Signing Key Sets",
    url: "signing-keys",
    icon: "key-round",
    subPages: {
      setDetail: {
        title: "Signing Key Set",
        url: ":setId",
        subPages: {
          overview: { title: "Overview", url: "overview" },
          keys: { title: "Keys", url: "keys" },
          settings: { title: "Settings", url: "settings" },
        },
      },
    },
  },
  auditLogs: {
    title: "Audit Logs",
    url: "audit-logs",
    icon: "history",
  },
  // Killswitches are managed on the identity of the person they restrict, so
  // this route no longer carries a roster: its index forwards to the people it
  // would have listed, and the detail record stays where the audit log links
  // to it.
  killswitch: {
    title: "Killswitch",
    url: "killswitch",
    icon: "shield-off",
    subPages: {
      detail: {
        title: "Killswitch detail",
        url: ":killswitchId",
      },
    },
  },
  legacyAgents: {
    title: "Agent Identity",
    url: "agent-management",
    legacyRedirect: true,
  },
  legacyMcpSessions: {
    title: "MCP Sessions",
    url: "mcp-sessions",
    legacyRedirect: true,
  },
  legacyRemoteIdentityProviders: {
    title: "Authorization Servers",
    url: "remote-identity-providers/*",
    legacyRedirect: true,
  },
  identity: {
    title: "IDP and SSO",
    url: "identity",
    icon: "fingerprint",
  },
  // Platform Admin pages — the former floating Developer Toolkit, one page per
  // old tab. Speakeasy staff only (plus local dev); see PlatformAdminGate.
  platformAdminOverview: {
    title: "Platform Admin Overview",
    url: "platform-admin",
    icon: "crown",
  },
  platformAdminRbac: {
    title: "RBAC Override",
    url: "platform-admin/rbac",
    icon: "shield",
  },
  platformAdminOnboarding: {
    title: "Enterprise Onboarding",
    url: "platform-admin/onboarding",
    icon: "mail",
  },
  platformAdminOpenRouterKeys: {
    title: "OpenRouter Keys",
    url: "platform-admin/openrouter-keys",
    icon: "key-round",
  },
  deviceAgent: {
    title: "Device Agent",
    url: "device-agent",
    icon: "laptop",
    subPages: {
      configuration: {
        title: "Configuration",
        url: "configuration",
      },
      scanTargets: {
        title: "Scan Targets",
        url: "scan-targets",
      },
      mdmIntegrations: {
        title: "MDM Integrations",
        url: "mdm-integrations",
      },
      mdmDetail: {
        title: "MDM Integration",
        url: "mdm-integrations/:provider",
      },
    },
  },
  access: {
    title: "Roles & Permissions",
    url: "access",
    icon: "shield",
    subPages: {
      roles: {
        title: "Roles & Permissions",
        url: "roles",
      },
      members: {
        title: "Roles & Permissions",
        url: "members",
      },
      assignments: {
        title: "MCP Assignments",
        url: "assignments",
      },
      challenges: {
        title: "Roles & Permissions",
        url: "challenges",
      },
    },
  },
  // Role authoring is its own page, not a sheet: a role can carry a dozen
  // permissions, each with rules of its own, which is more than a sheet can
  // show without scrolling away the thing being edited. These are siblings of
  // `access` rather than sub-pages because the access page renders tabs, not
  // an outlet.
  createRole: {
    title: "Create Role",
    url: "access/roles/create",
  },
  editRole: {
    title: "Edit Role",
    url: "access/roles/:roleId/edit",
  },
  requestAccess: {
    title: "Request Access",
    url: "request-access",
    outsideMainLayout: true,
  },
  setup: {
    title: "Setup",
    url: "setup",
    icon: "settings",
    outsideMainLayout: true,
  },
  // Legacy per-card pages (setup/idp, setup/wizard, ...) open the wizard on
  // that card.
  setupTask: {
    title: "Setup task",
    url: "setup/:taskSlug",
    icon: "list-checks",
    outsideMainLayout: true,
  },
  // Headless mode renders its own chrome (mode tabs only, no sidebar or
  // workspace header), so it sits outside OrgLayout.
  headless: {
    title: "Headless",
    url: "headless",
    outsideMainLayout: true,
  },
} satisfies Record<string, RouteEntry>;

export type OrgRouteStructure = typeof ORG_ROUTE_STRUCTURE;
type OrgRoutesWithGoTo = TransformRouteToGoTo<OrgRouteStructure>;

function routePaths(
  routes: Record<string, RouteEntry>,
  parent?: string,
): string[] {
  return Object.values(routes).flatMap((route) => {
    if (!route.url) return [];
    const path = parent ? `${parent}/${route.url}` : route.url;
    return [path, ...routePaths(route.subPages ?? {}, path)];
  });
}

/**
 * The org-relative path of every org-level route, nested pages included (for
 * redirect logic). A detail page such as "access-hub/:issuerId" has to be
 * listed with its parent, or its URL reads as a path inside a project that
 * happens to share the parent's slug.
 */
export const orgRoutePaths = routePaths(ORG_ROUTE_STRUCTURE);

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
