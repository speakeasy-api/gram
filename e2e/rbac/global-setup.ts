import { provisionWorld } from "./lib/personas.ts";

export default async function globalSetup(): Promise<void> {
  await provisionWorld();
}
