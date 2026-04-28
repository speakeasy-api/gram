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
}