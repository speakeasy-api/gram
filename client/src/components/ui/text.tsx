export function Text({
  variant,
  muted,
  children,
  className,
}: {
  variant: "subheading" | "body" | "caption";
  muted?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  switch (variant) {
    case "subheading":
      return <p className={`text-md font-light ${muted ? "text-muted-foreground" : "text-foreground"} ${className}`}>{children}</p>;
    case "body":
      return <p className={`text-base font-light ${muted ? "text-muted-foreground" : "text-foreground"} ${className}`}>{children}</p>;
    case "caption":
      return <p className={`text-sm font-light ${muted ? "text-muted-foreground" : "text-foreground"} ${className}`}>{children}</p>;
  }
}
