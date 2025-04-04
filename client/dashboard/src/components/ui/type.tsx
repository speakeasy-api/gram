export function Type({
  variant = "body",
  muted,
  children,
  className,
}: {
  variant?: "subheading" | "body" | "small";
  muted?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  switch (variant) {
    case "subheading":
      return (
        <p
          className={`text-md font-light ${
            muted ? "text-muted-foreground" : "text-foreground"
          } ${className}`}
        >
          {children}
        </p>
      );
    case "body":
      return (
        <p
          className={`text-base font-light ${
            muted ? "text-muted-foreground" : "text-foreground"
          } ${className}`}
        >
          {children}
        </p>
      );
    case "small":
      return (
        <p
          className={`text-sm font-light ${
            muted ? "text-muted-foreground" : "text-foreground"
          } ${className}`}
        >
          {children}
        </p>
      );
  }
}
