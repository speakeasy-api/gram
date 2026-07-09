import dynamicIconImports from "lucide-react/dynamicIconImports";
import customDynamicIconImports from "./customIcons";

const lucideIconNames = Object.keys(
  dynamicIconImports,
) as (keyof typeof dynamicIconImports)[];

const customIconNames = Object.keys(
  customDynamicIconImports,
) as (keyof typeof customDynamicIconImports)[];

export const iconNames = [...lucideIconNames, ...customIconNames];

export type IconName = (typeof iconNames)[number];
