const dynamicIconImports = {
  npm: (): Promise<typeof import("./npm")> => import("./npm"),
  pypi: (): Promise<typeof import("./pypi")> => import("./pypi"),
  nuget: (): Promise<typeof import("./nuget")> => import("./nuget"),
  go: (): Promise<typeof import("./go")> => import("./go"),
  gems: (): Promise<typeof import("./gems")> => import("./gems"),
  maven: (): Promise<typeof import("./maven")> => import("./maven"),
  packagist: (): Promise<typeof import("./packagist")> => import("./packagist"),
};

export { dynamicIconImports as default };
