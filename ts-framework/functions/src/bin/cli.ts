#!/usr/bin/env -S node --experimental-strip-types
import process from "node:process";
import { run } from "@stricli/core";
import { buildContext } from "../cli/context.ts";
import { app } from "../cli/app.ts";

run(app, process.argv.slice(2), buildContext(process));
