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