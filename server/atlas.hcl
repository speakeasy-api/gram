diff {
  concurrent_index {
    create = true
    add  = true
    drop = true
  }
}

lint {
  destructive {
    // Allow dropping tables or columns
    // that their name start with "drop_".
    allow_table {
      match = "drop_.+"
    }
    allow_column {
      match = "drop_.+"
    }
  }
  // PG110 reports non-optimal column alignment for byte padding.
  // We don't reorder columns for alignment.
  check "PG110" {
    skip = true
  }
}

docker "clickhouse" "dev" {
  image = "clickhouse/clickhouse-server:26.2.19.43@sha256:c2f2605585899d5103a0447daadbc0005f362200d5f0fcca7f40db3ca0dd36dd"
  // Keep server scope for marts, but replay unqualified application DDL in gram.
  baseline = <<SQL
    CREATE DATABASE gram;
    USE gram;
  SQL
}

data "composite_schema" "clickhouse" {
  schema {
    url = "file://clickhouse/schema.sql"
  }
  schema {
    url = "file://clickhouse/mart.sql"
  }
}

env "clickhouse" {
  dev = docker.clickhouse.dev.url
  schema {
    src = data.composite_schema.clickhouse.url
  }
  migration {
    dir = "file://clickhouse/migrations"
  }
}

env "clickhouse_golang_migrate" {
  dev = docker.clickhouse.dev.url
  schema {
    src = data.composite_schema.clickhouse.url
  }
  migration {
    dir = "file://clickhouse/local/golang_migrate?format=golang-migrate"
  }
}