import { createFileRoute } from "@tanstack/react-router";
import { EmaCanvas } from "@/components/ema/EmaCanvas";

export const Route = createFileRoute("/ema/")({
  component: EmaCanvas,
});
