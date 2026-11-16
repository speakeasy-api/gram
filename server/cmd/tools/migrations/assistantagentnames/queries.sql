WITH candidates AS MATERIALIZED (
 SELECT g.id, g.organization_id, g.project_id, a.name,
        'Assistant ' || a.id::text AS generated_name
 FROM agents AS g
 JOIN assistant_agent_bindings AS b
   ON b.agent_id = g.id
  AND b.organization_id = g.organization_id
  AND b.project_id = g.project_id
 JOIN assistants AS a
   ON a.id = b.assistant_id
  AND a.organization_id = b.organization_id
  AND a.project_id = b.project_id
 JOIN projects AS p
   ON p.id = b.project_id AND p.organization_id = b.organization_id
 WHERE NOT g.deleted AND NOT b.deleted AND NOT a.deleted AND NOT p.deleted
   AND b.project_ref_id = p.id AND b.project_ref_organization_id = p.organization_id
   AND g.name = 'Assistant ' || a.id::text
   AND g.name IS DISTINCT FROM a.name
   AND ($1::text = '' OR g.organization_id = $1::text)
   AND ($2::text = '' OR g.project_id = NULLIF($2::text, '')::uuid)
 ORDER BY g.id
 FOR UPDATE OF g, b, a, p
), updated AS (
 UPDATE agents AS g
 SET name = c.name, updated_at = clock_timestamp()
 FROM candidates AS c
 WHERE $3::boolean
   AND g.id = c.id AND g.organization_id = c.organization_id AND g.project_id = c.project_id
   AND g.name = c.generated_name AND NOT g.deleted
 RETURNING g.id
)
SELECT CASE WHEN $3::boolean THEN (SELECT count(*) FROM updated)
            ELSE (SELECT count(*) FROM candidates) END;
