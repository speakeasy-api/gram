import type { JSX } from "react";
// oxlint-disable-next-line no-restricted-imports -- object URLs are external resources requiring cleanup
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { adminIssuerImageQuery } from "@/lib/gramAdminClient";
export function IssuerLogo({ id }: { id: string }): JSX.Element | null {
  const query = useQuery(adminIssuerImageQuery(id));
  const [url, setUrl] = useState("");
  useEffect(() => {
    if (!query.data) return;
    const next = URL.createObjectURL(query.data);
    setUrl(next);
    return () => URL.revokeObjectURL(next);
  }, [query.data]);
  if (query.error)
    return (
      <span role="alert" className="text-muted-foreground text-xs">
        Logo unavailable
      </span>
    );
  return url ? (
    <img src={url} alt="Issuer logo" className="size-10 object-contain" />
  ) : null;
}
