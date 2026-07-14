-- drop "tum_breakdown_summaries_mv" view (IF EXISTS: local golang-migrate
-- lineages that ran an earlier revision of this drop may lack the objects)
DROP VIEW IF EXISTS `tum_breakdown_summaries_mv`;
-- drop "tum_breakdown_summaries" table
DROP TABLE IF EXISTS `tum_breakdown_summaries`;
