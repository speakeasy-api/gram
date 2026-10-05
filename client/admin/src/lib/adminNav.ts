// The destinations the admin app offers from anywhere: the sidebar's global nav
// and the command palette's "Go to" group are the same views.
//
// It sits in `lib` beside `organizationFilters.ts`, and for the same reason:
// two surfaces read it, and a list declared inside either one of them would
// leave the other importing from a component.
//
// `as const` keeps each `to` a literal, which is what the router types check the
// link and the navigation against.

import {
  BuildingIcon,
  UsersIcon,
  CalculatorIcon,
  FolderIcon,
  BookOpenIcon,
  Grid2X2Icon,
  KeyRoundIcon,
  PlugZapIcon,
  ListChecksIcon,
  RocketIcon,
} from "lucide-react";

import { McpIcon } from "@/components/ui/mcp-icon";

export const ADMIN_NAV_GROUPS = [
  {
    label: "Account Management",
    items: [
      {
        to: "/organizations",
        label: "Organizations",
        // Only the palette reads these. They are the words an operator types for a
        // view whose name they do not have in front of them, so a term that misses
        // the label still finds the page rather than reading as "no results".
        keywords: "orgs accounts customers tenants companies",
        icon: BuildingIcon,
      },
      {
        to: "/users",
        label: "Users",
        keywords: "people email members directory",
        icon: UsersIcon,
      },
      {
        to: "/projects",
        label: "Projects",
        keywords: "project lookup workspace",
        icon: FolderIcon,
      },
      {
        to: "/stoken-calculator",
        label: "S-token Calculator",
        keywords: "stoken tokens pricing usage estimate",
        icon: CalculatorIcon,
      },
    ],
  },
  {
    label: "Platform Management",
    items: [
      {
        to: "/registry",
        label: "MCP Registry",
        keywords: "catalog mcp servers",
        icon: McpIcon,
      },
      {
        to: "/integration-coverage",
        label: "Support matrix",
        keywords: "support matrix products capabilities integrations",
        icon: Grid2X2Icon,
      },
      {
        to: "/onboarding-steps",
        label: "Steps",
        keywords: "onboarding steps setup wizard cards groups playbooks",
        icon: ListChecksIcon,
      },
      {
        to: "/onboarding-playbooks",
        label: "Use Cases & Playbooks",
        keywords: "onboarding use cases playbooks outcomes default",
        icon: BookOpenIcon,
      },
      {
        to: "/hooks-rollout",
        label: "Hooks rollout",
        keywords: "hooks observability plugin version release pin posthog",
        icon: RocketIcon,
      },
      {
        to: "/remote-session-issuers",
        label: "Remote Session Issuers",
        keywords: "oauth identity providers issuers",
        icon: KeyRoundIcon,
      },
      {
        to: "/mcp-setup",
        label: "Admin MCP",
        keywords: "install connect agents claude codex cursor tailscale",
        icon: PlugZapIcon,
      },
    ],
  },
] as const;

export const ADMIN_NAV = ADMIN_NAV_GROUPS.map((group) => group.items).flat();
