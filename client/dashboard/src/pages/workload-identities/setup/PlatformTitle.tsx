import { isRelativePath } from "@/components/setup-steps/origin";

/** A catalog platform's name with its logo, for page titles. */
export function PlatformTitle({
  name,
  icon,
}: {
  name: string;
  icon?: string;
}): JSX.Element {
  return (
    <span className="inline-flex items-center gap-3">
      {icon !== undefined && isRelativePath(icon) && (
        <img src={icon} alt="" className="h-9 w-9 shrink-0" />
      )}
      {name}
    </span>
  );
}
