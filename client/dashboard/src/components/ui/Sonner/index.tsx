import { useConfig } from "@/components/ui/hooks/useConfig";
import { Toaster as Sonner, ToasterProps } from "sonner";

const Toaster = ({ ...props }: ToasterProps): JSX.Element => {
  // The app theme lives in ConfigContext, not next-themes (which has no
  // provider here and would fall back to the OS scheme).
  const { theme } = useConfig();

  return (
    <Sonner
      theme={theme}
      className="toaster group"
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
        } as React.CSSProperties
      }
      {...props}
    />
  );
};

export { Toaster };
