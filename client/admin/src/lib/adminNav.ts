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
  CalculatorIcon,
  FolderIcon,
  KeyRoundIcon,
} from "lucide-react";

export const ADMIN_NAV = [
  {
    to: "/remote-session-issuers",
    label: "Remote session issuers",
    keywords: "oauth identity providers issuers",
    icon: KeyRoundIcon,
  },
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
    to: "/projects",
    label: "Projects",
    keywords: "project lookup workspace",
    icon: FolderIcon,
  },
  {
    to: "/stoken-calculator",
    label: "S-token calculator",
    keywords: "stoken tokens pricing usage estimate",
    icon: CalculatorIcon,
  },
] as const;
