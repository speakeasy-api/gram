import { Icon, IconBook, IconCode, IconMessageChatbot } from "@tabler/icons-react";
import { IconSettings } from "@tabler/icons-react";
import { IconBlocks, IconTools } from "@tabler/icons-react";
import { IconDashboard } from "@tabler/icons-react";
import { IconCirclePlusFilled } from "@tabler/icons-react";
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

export const SanboxIcon = IconMessageChatbot;

export type AppRoute = {
  title: string;
  url: string;
  external?: boolean;
  icon?: Icon;
  component?: React.ComponentType;
  indexComponent?: React.ComponentType;
  subPages?: AppRoute[];
  active?: boolean;
};

export const ROUTES = {
  user: {
    name: "shadcn",
    email: "m@example.com",
    avatar: "/avatars/shadcn.jpg",
  },
  primaryCTA: {
    title: "Upload OpenAPI",
    url: "/upload",
    icon: IconCirclePlusFilled,
    component: Onboarding,
  },
  navMain: [
    {
      title: "Home",
      url: "/",
      icon: IconDashboard,
      component: Home,
    },
    {
      title: "Integrations",
      url: "/integrations",
      icon: IconBlocks,
      component: Integrations,
    },
    {
      title: "Toolsets",
      url: "/toolsets",
      icon: IconTools,
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
      icon: IconCode,
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
      title: "Sandbox",
      url: "/sandbox",
      icon: SanboxIcon,
      component: Sandbox,
    },
  ],
  navSecondary: [
    {
      title: "Settings",
      url: "/settings",
      icon: IconSettings,
      component: Settings,
    },
    {
      title: "Docs",
      url: "https://docs.speakeasy.com",
      icon: IconBook,
      external: true,
    },
  ],
};
