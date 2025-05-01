import Integrations from "./pages/integrations/Integrations";
import Toolsets, { ToolsetsRoot } from "./pages/toolsets/Toolsets";
import Home from "./pages/home/Home";
import Onboarding from "./pages/onboarding/Onboarding";
import ToolsetPage from "./pages/toolsets/Toolset";
import Sandbox from "./pages/sandbox/Sandbox";
import Settings from "./pages/settings/Settings";
import Environments from "./pages/environments/Environments";
import { EnvironmentsRoot } from "./pages/environments/Environments";
import EnvironmentPage from "./pages/environments/Environment";
import Login from "./pages/login/Login";
import SDK from "./pages/sdk/SDK";
import { IconProps } from "@speakeasy-api/moonshine";

export type AppRoute = {
  title: string;
  url: string;
  external?: boolean;
  icon?: IconProps["name"];
  component?: React.ComponentType;
  indexComponent?: React.ComponentType;
  subPages?: AppRoute[];
  active?: boolean;
};

export const ROUTES = {
  unauthenticatedRoutes: [
    {
      title: "Login",
      url: "/login",
      component: Login,
    },
  ],
  primaryCTA: [
    {
      title: "Sandbox",
      url: "/sandbox",
      icon: "message-circle",
      component: Sandbox,
    },
  ],
  navMain: [
    {
      title: "Home",
      url: "/",
      icon: "circle-gauge",
      component: Home,
    },
    {
      title: "Integrations",
      url: "/integrations",
      icon: "blocks",
      component: Integrations,
    },
    {
      title: "Toolsets",
      url: "/toolsets",
      icon: "pencil-ruler",
      component: ToolsetsRoot,
      indexComponent: Toolsets,
      subPages: [
        {
          title: "Toolset",
          url: ":toolsetSlug",
          component: ToolsetPage,
        },
      ],
    },
    {
      title: "Environments",
      url: "/environments",
      icon: "globe",
      component: EnvironmentsRoot,
      indexComponent: Environments,
      subPages: [
        {
          title: "Environment",
          url: ":environmentSlug",
          component: EnvironmentPage,
        },
      ],
    },
    {
      title: "SDK",
      url: "/sdk",
      icon: "code",
      component: SDK,
    },
  ],
  navSecondary: [
    {
      title: "Upload OpenAPI",
      url: "/onboarding",
      icon: "upload",
      component: Onboarding,
    },
    {
      title: "Settings",
      url: "/settings",
      icon: "settings",
      component: Settings,
    },
    {
      title: "Docs",
      url: "https://docs.speakeasy.com",
      icon: "book-open",
      external: true,
    },
  ],
} as const satisfies Record<string, AppRoute[]>;
