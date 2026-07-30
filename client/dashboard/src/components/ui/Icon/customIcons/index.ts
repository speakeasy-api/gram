import type createCustomLucideIcon from "./createCustomLucideIcon";

type CustomIconModule = {
  default: ReturnType<typeof createCustomLucideIcon>;
};

/** Lazily-loaded brand icons that lucide does not ship. */
const dynamicIconImports = {
  npm: (): Promise<CustomIconModule> => import("./npm"),
  pypi: (): Promise<CustomIconModule> => import("./pypi"),
  nuget: (): Promise<CustomIconModule> => import("./nuget"),
  go: (): Promise<CustomIconModule> => import("./go"),
  gems: (): Promise<CustomIconModule> => import("./gems"),
  maven: (): Promise<CustomIconModule> => import("./maven"),
  packagist: (): Promise<CustomIconModule> => import("./packagist"),
};

export { dynamicIconImports as default };
