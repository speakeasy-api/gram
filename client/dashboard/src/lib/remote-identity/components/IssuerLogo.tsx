import { AssetImage } from "@/components/asset-image";
import { useIdentityTint } from "@/components/gradient-colors";
import { cn } from "@/lib/utils";

// Each size is a height. A logo keeps it and grows sideways, so a wordmark
// reads at full height instead of shrinking into a square.
const LOGO_SIZE = {
  sm: "h-5 min-w-5 max-w-12",
  md: "h-8 min-w-8 max-w-32",
  lg: "h-10 min-w-10 max-w-40",
  xl: "h-12 min-w-12 max-w-48",
} as const;

// Without a logo, a square tile of the same height holds the initial.
const INITIAL_SIZE = {
  sm: "size-5 text-xs",
  md: "size-8 text-sm",
  lg: "size-10 text-base",
  xl: "size-12 text-lg",
} as const;

/**
 * An identity provider's logo, contained so non-square logos keep their
 * shape. Without a logo it shows the provider's initial on its identity tint,
 * like the app's other initials.
 */
export function IssuerLogo({
  logoAssetId,
  name,
  size = "lg",
  className,
}: {
  logoAssetId: string | null | undefined;
  /** The provider's display name, whose first letter stands in for a logo. */
  name: string;
  size?: keyof typeof LOGO_SIZE;
  className?: string;
}): JSX.Element {
  const tint = useIdentityTint(name);

  if (!logoAssetId) {
    return (
      <span
        aria-hidden
        style={tint}
        className={cn(
          "flex shrink-0 items-center justify-center font-medium uppercase",
          INITIAL_SIZE[size],
          className,
        )}
      >
        {name.trim().charAt(0)}
      </span>
    );
  }

  return (
    <span
      className={cn(
        "flex shrink-0 items-center justify-center",
        LOGO_SIZE[size],
        className,
      )}
    >
      <AssetImage
        assetId={logoAssetId}
        alt=""
        className="h-full w-auto max-w-full object-contain"
      />
    </span>
  );
}
