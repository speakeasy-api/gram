import ws from "ws";
import { Pool, neonConfig } from "@neondatabase/serverless";

// Configure Neon for PlanetScale compatibility
neonConfig.webSocketConstructor = ws;
neonConfig.pipelineConnect = false;
neonConfig.wsProxy = (host, port) => `${host}/v2?address=${host}:${port}`;

export type PlanetScaleConfig = {
  host: string;
  port: string;
  database: string;
  username: string;
  password: string;
};

export type ExecuteQueryParams = {
  query: string;
  args?: Array<string | number | boolean | null>;
};

export type QueryResult = {
  rows: Array<Record<string, unknown>>;
  rowCount?: number;
  fields?: Array<{
    name: string;
  }>;
};

let cachedPool: Pool | null = null;

function getPool(cfg: PlanetScaleConfig): Pool {
  if (cachedPool) {
    return cachedPool;
  }

  const connectionString = `postgresql://${cfg.username}:${cfg.password}@${cfg.host}:${cfg.port}/${cfg.database}`;

  cachedPool = new Pool({ connectionString });

  return cachedPool;
}

export async function executeQuery(
  cfg: PlanetScaleConfig,
  params: ExecuteQueryParams,
): Promise<QueryResult> {
  const pool = getPool(cfg);

  const result = await pool.query(params.query, params.args);

  return {
    rows: result.rows as Array<Record<string, unknown>>,
    rowCount: result.rowCount ?? undefined,
    fields: result.fields?.map((field) => ({
      name: field.name,
    })),
  };
}
