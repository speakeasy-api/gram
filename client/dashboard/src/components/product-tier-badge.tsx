import { useSession } from "@/contexts/Auth";

export const ProductTierBadge = () => {
  const session = useSession();

  const name = {
    "free": "Free",
    "pro": "Pro",
    "enterprise": "Enterprise",
  }[session.gramAccountType];

  const classes = {
    "free": "bg-gray-100 text-gray-800",
    "pro": "bg-blue-100 text-blue-800",
    "enterprise": "bg-green-100 text-green-800",
  }[session.gramAccountType];

  return <div className={`text-xs text-muted-foreground px-1 py-0.5 rounded-md ${classes}`}>{name}</div>;
};