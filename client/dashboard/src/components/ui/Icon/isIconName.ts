import { IconName, iconNames } from "./names";

const iconNameSet = new Set(Object.values(iconNames));

export function isIconName(maybeIconName: unknown): maybeIconName is IconName {
  return (
    typeof maybeIconName === "string" &&
    iconNameSet.has(maybeIconName as IconName)
  );
}
