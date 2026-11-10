import { snowflakeSetupUrl } from "@/pages/catalog/snowflake-install";
import { StrictMode, type Dispatch, type PropsWithChildren } from "react";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader";
import type { McpServer } from "@gram/client/models/components/mcpserver";
import {
  names,
  type Action,
  type State,
} from "@/pages/catalog/provider-setup/state";
import { ProviderSetupPage } from "./ProviderSetupPage";

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    metaMcp: {
      listMembers: mocks.members,
      addMember: mocks.addMember,
    },
  }),
}));
vi.mock("@gram/client/react-query/metaMcpMembers", () => ({
  invalidateAllMetaMcpMembers: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
const mocks = vi.hoisted(() => ({
  fire: Object.assign(vi.fn<(options: Record<string, unknown>) => void>(), {
    reset: vi.fn<() => void>(),
  }),
  create: vi.fn(),
  dispatch: vi.fn<Dispatch<Action>>(),
  provider: "github",
  snowflakeUrl:
    "https://example.snowflakecomputing.com/api/v2/databases/APP/schemas/PUBLIC/mcp-servers/MCP",
  initiallyValidated: false,
  pending: "",
  error: "",
  loading: false,
  loadProgress: vi.fn(),
