/**
 * Icon rendering for DATA-DRIVEN icon names (route configs, API-provided
 * category icons, command-palette entries). Statically known icons must
 * import the lucide component directly instead — tree-shakeable and typed:
 *
 *   import { Settings } from "lucide-react";
 *
 * DynamicIcon lazily code-splits each icon and also serves the custom
 * package-manager brand glyphs (npm, pypi, go, gems, maven, nuget,
 * packagist).
 */
export {
  Icon as DynamicIcon,
  type IconProps as DynamicIconProps,
} from "@/components/ui/icon";
export { type IconName } from "@/components/ui/icon/names";
