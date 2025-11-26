import { createClient } from "@clickhouse/client-web";
import type { ClickHouseClient } from "@clickhouse/client-web";

export type ClickHouseConfig = {
  host: string;
  port: string;
  database: string;
  username: string;
  password: string;
};

export type ExecuteQueryParams = {
  query: string;
  params?: Record<string, string | number | boolean | null>;
};

export type QueryResult = {
  rows: Array<Record<string, unknown>>;
  rowCount: number;
};

let cachedClient: ClickHouseClient | null = null;

function getClient(cfg: ClickHouseConfig): ClickHouseClient {
  if (cachedClient) {
    return cachedClient;
  }

  cachedClient = createClient({
    url: `http://${cfg.host}:${cfg.port}`,
    username: cfg.username,
    password: cfg.password,
    database: cfg.database,
  });

  return cachedClient;
}

export async function executeQuery(
  cfg: ClickHouseConfig,
  params: ExecuteQueryParams,
): Promise<QueryResult> {
  const client = getClient(cfg);

  const resultSet = await client.query({
    query: params.query,
    format: "JSONEachRow",
    query_params: params.params,
  });

  const data = await resultSet.json();

  return {
    rows: data as Array<Record<string, unknown>>,
    rowCount: data.length,
  };
}
