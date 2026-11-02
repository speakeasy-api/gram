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
  ChartColumnStackedIcon,
  ArrowRightLeftIcon,
  ActivityIcon,
  FlaskConicalIcon,
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
        to: "/signup-offers",
        label: "Signup offers",
        keywords: "signup stripe promotion tailscale partner gateway",
        icon: BookOpenIcon,
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
        to: "/remote-session-issuers",
        label: "Remote Session Issuers",
        keywords: "oauth identity providers issuers",
        icon: KeyRoundIcon,
      },
      {
        to: "/upstream-oauth-migration",
        label: "Upstream OAuth migration",
        keywords: "oauth clients associations credentials backfill migrate",
        icon: KeyRoundIcon,
      },
      {
        to: "/external-mcp-migration",
        label: "External MCP migration",
        keywords: "external mcp attachments remote servers migrate cutover",
        icon: ArrowRightLeftIcon,
      },
      {
        to: "/memory-evals",
        label: "Team memory evals",
        keywords:
          "memory eval evals benchmark models summary digest accuracy compare",
        icon: FlaskConicalIcon,
      },
      {
        to: "/fleet-health",
        label: "Fleet health",
        keywords:
          "hook clients curl binary versions sessions inference device agents speakeasyd rollout",
        icon: ActivityIcon,
      },
      {
        to: "/mcp-setup",
        label: "Admin MCP",
        keywords: "install connect agents claude codex cursor tailscale",
        icon: PlugZapIcon,
      },
      {
        to: "/customer-usage",
        label: "Customer usage",
        keywords: "spend billing usage customers paying growth estimate",
        icon: ChartColumnStackedIcon,
        // Rendered after the external Demo organization link rather than in
        // array order, so the sidebar lists it below that link.
        afterDemoOrganization: true,
      },
    ],
  },
] as const;

export const ADMIN_NAV = ADMIN_NAV_GROUPS.map((group) => group.items).flat();
