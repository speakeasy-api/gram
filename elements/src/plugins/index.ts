import type { Plugin } from "@/types/plugins";
import { chart } from "./chart";
import { generativeUI } from "./generative-ui";

export type PluginList = Plugin[] & {
  except(...ids: string[]): Plugin[];
};

function createPluginList(plugins: Plugin[]): PluginList {
  const arr = [...plugins] as PluginList;
  arr.except = (...ids: string[]) => {
    const excluded = new Set(ids);
    return arr.filter((p) => !excluded.has(p.id ?? p.language));
  };
  return arr;
}

export const recommended: PluginList = createPluginList([chart, generativeUI]);
export { chart } from "./chart";
export { generativeUI } from "./generative-ui";

export type { Plugin } from "@/types/plugins";
