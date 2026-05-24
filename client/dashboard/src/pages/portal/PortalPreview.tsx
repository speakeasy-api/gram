import { memo } from "react";

interface Props {
  orgSlug: string;
  projectSlug: string;
  className?: string;
}

export const PortalPreview = memo(function PortalPreview({
  orgSlug,
  projectSlug,
  className,
}: Props) {
  const src = `/${orgSlug}/projects/${projectSlug}/portal?preview=1`;
  return <iframe src={src} className={className} title="Portal preview" />;
});
