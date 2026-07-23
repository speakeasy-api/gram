-- create "chat_analysis_scores" table
CREATE TABLE `chat_analysis_scores` (
  `id` UUID COMMENT 'Producer-supplied score identifier, the queue evaluation id.',
  `created_at` DateTime64(9) COMMENT 'Time the score was completed.' CODEC(DoubleDelta, ZSTD(1)),
  `inserted_at` DateTime64(9) DEFAULT now64(9) COMMENT 'Time the score was inserted.' CODEC(DoubleDelta, ZSTD(1)),
  `organization_id` String COMMENT 'Organization the score belongs to.' CODEC(ZSTD(1)),
  `project_id` String DEFAULT '' COMMENT 'Project the score belongs to when known.' CODEC(ZSTD(1)),
  `chat_id` String COMMENT 'Gram chat identifier of the analyzed session.' CODEC(ZSTD(1)),
  `judge` LowCardinality(String) COMMENT 'Name of the analysis judge that produced the score.',
  `score` Float64 COMMENT 'Headline metric of the verdict, meaning defined per judge.',
  `detail` String COMMENT 'Full structured verdict as JSON, shape defined per judge.' CODEC(ZSTD(1)),
  `judge_model` LowCardinality(String) COMMENT 'Model used to judge the session.',
  `judge_prompt_version` LowCardinality(String) COMMENT 'Judge prompt version.',
  CONSTRAINT `score_valid` CHECK (isFinite(score)),
  CONSTRAINT `judge_valid` CHECK (judge != '')
) ENGINE = MergeTree
PRIMARY KEY (`organization_id`, `project_id`, `judge`, `created_at`, `id`) ORDER BY (`organization_id`, `project_id`, `judge`, `created_at`, `id`) PARTITION BY (toYYYYMM(created_at)) TTL toDateTime(created_at) + toIntervalDay(730) SETTINGS index_granularity = 8192 COMMENT 'Chat session analysis verdicts produced by the chat analysis judges.';
