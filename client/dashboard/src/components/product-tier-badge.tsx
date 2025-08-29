import { useSession } from "@/contexts/Auth";

export const ProductTierBadge = () => {
  const session = useSession();

  const name = {
    "free": "Free",
    "pro": "Pro",
    "enterprise": "Enterprise",
  }[session.gramAccountType];

  const classes = {
    "free": "bg-neutral-700 text-white",
    "pro": "bg-violet-500 text-white",
    "enterprise": "bg-success-foreground text-success",
  }[session.gramAccountType];

  return <div className={`text-xs text-muted-foreground px-1 py-0.5 rounded-sm ${classes}`}>{name}</div>;
};