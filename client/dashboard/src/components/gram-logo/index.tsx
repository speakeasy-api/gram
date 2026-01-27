import { cn } from "@/lib/utils";
import { GramLogoHorizontal } from "./variants/horizontal";
import { GramIcon } from "./variants/icon";
import { GramLogoVertical } from "./variants/vertical";

export const GramLogo = ({
  variant = "horizontal",
  className,
}: {
  variant?: "horizontal" | "vertical" | "icon";
  className?: string;
}) => {
  const variantsMap = {
    horizontal: GramLogoHorizontal,
    vertical: GramLogoVertical,
    icon: GramIcon,
  };
  return (
    <div className={cn("dark:invert flex items-center", className)}>
      {variantsMap[variant]()}
    </div>
  );
};
