#!/usr/bin/env node
"use strict";

const path = require("node:path");
const { spawn } = require("node:child_process");

const bin = process.platform === "win32" ? "lazyagent.exe" : "lazyagent";
const binaryPath = path.join(__dirname, "..", "vendor", bin);

const child = spawn(binaryPath, process.argv.slice(2), { stdio: "inherit" });

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
  } else {
    process.exit(code ?? 1);
  }
});
