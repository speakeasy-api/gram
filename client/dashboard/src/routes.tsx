import { Icon, IconName, IconProps } from "@speakeasy-api/moonshine";
import React, { useMemo } from "react";
import { Link, useLocation, useNavigate } from "react-router";
import { useSlugs } from "./contexts/Sdk";
import { cn } from "./lib/utils";
import Deployment from "./pages/deployments/deployment/Deployment";
import Deployments, { DeploymentsRoot } from "./pages/deployments/Deployments";
import EnvironmentPage from "./pages/environments/Environment";
import Environments, {
  EnvironmentsRoot,
} from "./pages/environments/Environments";
import Home from "./pages/home/Home";
import Integrations from "./pages/integrations/Integrations";
import Login from "./pages/login/Login";
import Register from "./pages/login/Register";
import { MCPDetailPage, MCPDetailsRoot } from "./pages/mcp/MCPDetails";
import { MCPHostedPage } from "./pages/mcp/MCPHostedPage";
import { MCPOverview, MCPRoot } from "./pages/mcp/MCPOverview";
import UploadOpenAPI from "./pages/onboarding/UploadOpenAPI";
import { OnboardingWizard } from "./pages/onboarding/Wizard";
import Playground from "./pages/playground/Playground";
import NewPromptPage from "./pages/prompts/NewPrompt";
import PromptPage from "./pages/prompts/Prompt";
import Prompts, { PromptsRoot } from "./pages/prompts/Prompts";
import SDK from "./pages/sdk/SDK";
import Settings from "./pages/settings/Settings";
import CustomTools, { CustomToolsRoot } from "./pages/toolBuilder/CustomTools";
import {
  ToolBuilderNew,
  ToolBuilderPage,
} from "./pages/toolBuilder/ToolBuilder";
import ToolsetPage, { ToolsetRoot } from "./pages/toolsets/Toolset";
import Toolsets, { ToolsetsRoot } from "./pages/toolsets/Toolsets";
import Billing from "./pages/billing/Billing";
import Telemetry from "./pages/telemetry/Telemetry";
import { SourcesRoot, SourcesPage } from "./pages/sources/Sources";
import SourceDetails from "./pages/sources/SourceDetails";
import Catalog, { CatalogRoot } from "./pages/catalog/Catalog";

type AppRouteBasic = {
  title: string;
  url: string;
  external?: boolean;
  icon?: IconName;
  component?: React.ComponentType;
  indexComponent?: React.ComponentType;
  subPages?: AppRoutesBasic;
  unauthenticated?: boolean;
  outsideMainLayout?: boolean;
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
  onboarding: {
    title: "Onboarding",
    url: "onboarding",
    component: OnboardingWizard,
    outsideMainLayout: true, // Break out of normal page structure
  },
  home: {
    title: "Home",
    url: "",
    icon: "house",
    component: Home,
  },
  playground: {
    title: "Playground",
    url: "playground",
    icon: "message-circle",
    component: Playground,
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
  toolsets: {
    title: "Toolsets",
    url: "toolsets",
    icon: "blocks",
    component: ToolsetsRoot,
    indexComponent: Toolsets,
    subPages: {
      toolset: {
        title: "Toolset",
        url: ":toolsetSlug",
        component: ToolsetRoot,
        indexComponent: ToolsetPage,
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
    },
  },
  catalog: {
    title: "Catalog",
    url: "catalog",
    icon: "store",
    component: CatalogRoot,
    indexComponent: Catalog,
  },
  mcp: {
    title: "MCP",
    url: "mcp",
    icon: "network",
    component: MCPRoot,
    indexComponent: MCPOverview,
    subPages: {
      details: {
        title: "MCP Details",
        url: ":toolsetSlug",
        component: MCPDetailsRoot,
        indexComponent: MCPDetailPage,
        subPages: {
          hosted_page: {
            title: "Hosted MCP Page",
            url: "page",
            component: MCPHostedPage,
          },
        },
      },
    },
  },
  logs: {
    title: "Logs",
    url: "logs",
    icon: "activity",
    component: Telemetry,
  },
  environments: {
    title: "Environments",
    url: "environments",
    icon: "globe",
    component: EnvironmentsRoot,
    indexComponent: Environments,
    subPages: {
      environment: {
        title: "Environment",
        url: ":environmentSlug",
        component: EnvironmentPage,
      },
    },
  },
  sdks: {
    title: "SDKs",
    url: "sdks",
    icon: "code",
    component: SDK,
  },
  uploadOpenAPI: {
    title: "Upload OpenAPI",
    url: "upload",
    icon: "upload",
    component: UploadOpenAPI,
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
  settings: {
    title: "Settings",
    url: "settings",
    icon: "settings",
    component: Settings,
  },
  billing: {
    title: "Billing",
    url: "billing",
    icon: "credit-card",
    component: Billing,
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

export const useRoutes = (): RoutesWithGoTo => {
  const location = useLocation();
  const { orgSlug, projectSlug } = useSlugs();
  const navigate = useNavigate();

  // Check if the current url matches the route url, including dynamic segments
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
      parent = `/:orgSlug/:projectSlug`;
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
              return `/${orgSlug}/${projectSlug}`;
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
      navigate(resolveUrl(...params));
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
      Icon: (props: Omit<IconProps, "name">) =>
        route.icon ? <Icon {...props} name={route.icon} /> : null,
      href: resolveUrl,
      goTo,
      Link: linkComponent,
      ...subPages,
    };

    if (route.url.startsWith("/")) {
      newRoute.goTo = () => route.url;
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
    [location.pathname],
  );

  return routes;
};
