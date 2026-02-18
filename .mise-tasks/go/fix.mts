#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Run go fix on the specified files"
//MISE dir="{{ config_root }}"

//USAGE arg "<files>..." help="Go files to fix"

import path from "node:path";
import { $ } from "zx";

function run() {
  const cwd = process.cwd();
  const files = process.argv.slice(2);
  let dirs = files.map((f) => {
    const relpath = path.relative(cwd, path.dirname(path.resolve(f)));
    return relpath.startsWith("..") ? relpath : `./${relpath}`;
  });
  dirs = [...new Set(dirs)];

  $.sync`go fix ${dirs}`;
}

run();

// set -e

// # the variable is `$usage_files`. parse it as an array.
// IFS=' ' read -ra files <<< "$usage_files"

// # get unique directories
// declare -A seen
// dirs=()
// for f in "${files[@]}"; do
//   d=$(dirname "$f")
//   d="${d#./}"
//   if [[ -z "${seen[$d]+_}" ]]; then
//     seen[$d]=1
//     dirs+=("$d")
//   fi
// done

// go fix "${dirs[@]/#/./}"
