interface Props {
  projectSlug: string;
  className?: string;
}

export function PortalPreview({ projectSlug, className }: Props) {
  const src = `/portal/${projectSlug}?preview=1`;
  return <iframe src={src} className={className} title="Portal preview" />;
}
