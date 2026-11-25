import { createFileRoute } from "@tanstack/react-router";
import { UsersList } from "@/pages/users";
import { usersSearchSchema } from "@/lib/usersSearchRoute";
export const Route = createFileRoute("/users/")({
  component: UsersList,
  validateSearch: usersSearchSchema,
});
