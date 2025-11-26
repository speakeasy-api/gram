import { createClient } from "@clickhouse/client-web";

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

export async function executeQuery(
  cfg: ClickHouseConfig,
  params: ExecuteQueryParams,
): Promise<QueryResult> {
  const client = createClient({
    url: `http://${cfg.host}:${cfg.port}`,
    username: cfg.username,
    password: cfg.password,
    database: cfg.database,
  });

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
