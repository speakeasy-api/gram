import { Stack } from "@speakeasy-api/moonshine";
import { Link } from "react-router";

export const NameAndSlug = ({
  name,
  slug,
  linkTo,
}: {
  name: string;
  slug: string;
  linkTo?: string;
}) => {
  const title = linkTo ? (
    <Link to={linkTo} className="hover:underline">
      <span>{name}</span>
    </Link>
  ) : (
    <span>{name}</span>
  );

  const slugEquivalent = slug.toLowerCase() === name.toLowerCase();

  return (
    <Stack direction="horizontal" gap={2}>
      {title}
      {!slugEquivalent && (
        <span className="text-muted-foreground">({slug})</span>
      )}
    </Stack>
  );
};
