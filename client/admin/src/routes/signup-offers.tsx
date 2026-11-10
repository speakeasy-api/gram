import { createFileRoute } from "@tanstack/react-router";
import { SignupOffersPage } from "@/pages/SignupOffers";

export const Route = createFileRoute("/signup-offers")({
  component: SignupOffersPage,
  staticData: { crumb: "Signup offers" },
});
