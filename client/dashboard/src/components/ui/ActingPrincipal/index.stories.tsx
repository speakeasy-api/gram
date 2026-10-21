import type { Meta, StoryObj } from "@storybook/react-vite";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { GramCore } from "@gram/client/core.js";
import { GramProvider } from "@gram/client/react-query/_context.js";
import {
  queryKeyGrants,
  type GrantsQueryData,
} from "@gram/client/react-query/grants.js";
import { ActingPrincipal } from ".";

// Both halves link to an identity page, and building the person's href goes
// through RBAC, which reads the SDK. Nothing here calls the network — the
// client exists so the hooks have something to hold, and the grants the gate
// reads are seeded instead of fetched. Without the seed every "linked" story
// rendered plain text, since a disabled query is idle rather than loading and
// an empty grant list holds no org:read.
const client = new GramCore();
const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, enabled: false } },
});
// The session is empty outside the app's auth provider, which is the session
// the grants query keys on here.
queryClient.setQueryData(queryKeyGrants({ gramSession: "" }), {
  grants: [{ scope: "org:read" }],
} satisfies GrantsQueryData);

const meta: Meta<typeof ActingPrincipal> = {
  title: "Design System/ActingPrincipal",
  component: ActingPrincipal,
  parameters: {
    docs: {
      description: {
        component:
          "Who did a thing, when an agent did it for someone. Naming only one of the two is wrong in both directions: the agent alone hides who is accountable, the person alone hides that a machine made the call.",
      },
    },
  },
  decorators: [
    (Story) => (
      <QueryClientProvider client={queryClient}>
        <GramProvider client={client}>
          <MemoryRouter>
            <div className="m-auto flex max-w-xl flex-col gap-4 p-6 text-sm">
              <Story />
            </div>
          </MemoryRouter>
        </GramProvider>
      </QueryClientProvider>
    ),
  ],
};

export default meta;

type Story = StoryObj<typeof ActingPrincipal>;

export const OnBehalfOf: Story = {
  args: {
    agent: {
      id: "00000000-0000-4000-a000-000000000001",
      name: "Release assistant",
    },
    authorizer: {
      id: "user_locl_amara",
      name: "Amara Okafor",
      identity: { userId: "user_locl_amara" },
    },
    // Linking is opt-in, so a story showing it has to ask for it.
    linked: true,
  },
};

/** A key or a federated platform signs in as the agent; no human stands behind the call. */
export const AgentAlone: Story = {
  args: {
    agent: {
      id: "00000000-0000-4000-a000-000000000001",
      name: "Release assistant",
    },
    linked: true,
  },
};

/** Neither name resolved. The identifiers are still true, and still link. */
export const UnresolvedNames: Story = {
  args: {
    agent: { id: "00000000-0000-4000-a000-000000000001" },
    authorizer: {
      id: "user_locl_amara",
      identity: { userId: "user_locl_amara" },
    },
    linked: true,
  },
};

/** Inside a row that is already a click target, where a nested link would swallow the outer one. */
export const Unlinked: Story = {
  args: {
    agent: {
      id: "00000000-0000-4000-a000-000000000001",
      name: "Release assistant",
    },
    authorizer: { id: "user_locl_amara", name: "Amara Okafor" },
    linked: false,
  },
};

/** A long agent name beside a long person name, in a narrow column. */
export const Truncating: Story = {
  args: {
    agent: {
      id: "00000000-0000-4000-a000-000000000001",
      name: "Quarterly revenue reconciliation and reporting assistant",
    },
    authorizer: {
      id: "user_locl_amara",
      name: "Amara Okafor-Lindqvist",
      identity: { userId: "user_locl_amara" },
    },
    linked: true,
  },
  decorators: [
    (Story) => (
      <div className="border-border w-64 border p-3">
        <Story />
      </div>
    ),
  ],
};
