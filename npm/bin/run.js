#!/usr/bin/env node
const path = require("path");
const { execFileSync } = require("child_process");

const ext = process.platform === "win32" ? ".exe" : "";
const binary = path.join(__dirname, "recall-mcp-gateway" + ext);

try {
  execFileSync(binary, process.argv.slice(2), { stdio: "inherit" });
} catch (err) {
  if (err.status !== undefined) process.exit(err.status);
  console.error(
    "recall-mcp-gateway binary not found. Run: npm install @controlisai/recall-mcp-gateway"
  );
  process.exit(1);
}
