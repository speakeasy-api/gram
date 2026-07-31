import dynamicIconImports from "lucide-react/dynamicIconImports";

const lucideIconNames = Object.keys(
  dynamicIconImports,
) as (keyof typeof dynamicIconImports)[];

export const iconNames = lucideIconNames;

export type IconName = (typeof iconNames)[number];
