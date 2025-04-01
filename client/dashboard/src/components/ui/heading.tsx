export function Heading({
  variant,
  children,
  className,
}: {
  variant: "h1" | "h2" | "h3" | "h4" | "h5" | "h6";
  children: React.ReactNode;
  className?: string;
}) {
  switch (variant) {
    case "h1":
      return (
        <h1 className={`text-3xl font-light font-[Mona_Sans] ${className}`}>{children}</h1>
      );
    case "h2":
      return (
        <h2 className={`text-2xl font-light font-[Mona_Sans] ${className}`}>{children}</h2>
      );
    case "h3":
      return (
        <h3 className={`text-xl font-light font-[Mona_Sans] ${className}`}>{children}</h3>
      );
    case "h4":
      return (
        <h4 className={`text-lg font-light font-[Mona_Sans] ${className}`}>{children}</h4>
      );
    case "h5":
      return (
        <h5 className={`text-base font-light font-[Mona_Sans] ${className}`}>{children}</h5>
      );
    case "h6":
      return (
        <h6 className={`text-sm uppercase font-medium font-[Mona_Sans] ${className}`}>{children}</h6>
      );
  }
}
