#!/usr/bin/env node
// Downloads the prebuilt lazyagent binary from GitHub Releases for the
// current platform/arch, verifies its checksum, and extracts it into
// `vendor/` next to this script. The bin shim (`bin/lazyagent.js`) then
// execs that binary.
//
// Why a postinstall downloader: the Go binary is published by goreleaser
// as per-OS/arch archives, and fetching on install lets the npm package
// stay small and platform-agnostic without having to ship per-platform
// subpackages.

"use strict";

const fs = require("node:fs");
const path = require("node:path");
const https = require("node:https");
const crypto = require("node:crypto");
const { spawnSync } = require("node:child_process");

const pkg = require("./package.json");

const REPO = "chojs23/lazyagent";
const VERSION = pkg.version;
const VENDOR_DIR = path.join(__dirname, "vendor");

function log(msg) {
  process.stderr.write(`[lazyagent] ${msg}\n`);
}

function die(msg) {
  log(`error: ${msg}`);
  process.exit(1);
}

function detectTarget() {
  const platforms = { linux: "linux", darwin: "darwin", win32: "windows" };
  const arches = { x64: "amd64", arm64: "arm64" };
  const platform = platforms[process.platform];
  const arch = arches[process.arch];
  if (!platform) die(`unsupported OS: ${process.platform}`);
  if (!arch) die(`unsupported CPU arch: ${process.arch}`);
  return {
    platform,
    arch,
    ext: platform === "windows" ? "zip" : "tar.gz",
    binaryName: platform === "windows" ? "lazyagent.exe" : "lazyagent",
  };
}

function assetUrl(target) {
  const file = `lazyagent_${VERSION}_${target.platform}_${target.arch}.${target.ext}`;
  return {
    archive: `https://github.com/${REPO}/releases/download/v${VERSION}/${file}`,
    checksums: `https://github.com/${REPO}/releases/download/v${VERSION}/checksums.txt`,
    file,
  };
}

function download(url, dest) {
  return new Promise((resolve, reject) => {
    const visit = (current, redirects) => {
      if (redirects > 10) return reject(new Error("too many redirects"));
      https
        .get(current, (res) => {
          if (
            res.statusCode &&
            res.statusCode >= 300 &&
            res.statusCode < 400 &&
            res.headers.location
          ) {
            res.resume();
            const next = new URL(res.headers.location, current).toString();
            return visit(next, redirects + 1);
          }
          if (res.statusCode !== 200) {
            res.resume();
            return reject(new Error(`GET ${current} -> ${res.statusCode}`));
          }
          const file = fs.createWriteStream(dest);
          res.pipe(file);
          file.on("finish", () => file.close(() => resolve()));
          file.on("error", reject);
        })
        .on("error", reject);
    };
    visit(url, 0);
  });
}

function sha256File(p) {
  const hash = crypto.createHash("sha256");
  hash.update(fs.readFileSync(p));
  return hash.digest("hex");
}

function verifyChecksum(archivePath, checksumsPath, archiveName) {
  // checksums.txt format: "<sha256>  <filename>" per line
  const lines = fs.readFileSync(checksumsPath, "utf8").split(/\r?\n/);
  const match = lines
    .map((l) => l.trim())
    .find((l) => l.endsWith(`  ${archiveName}`) || l.endsWith(` ${archiveName}`));
  if (!match) die(`checksum for ${archiveName} not found in checksums.txt`);
  const expected = match.split(/\s+/)[0];
  const actual = sha256File(archivePath);
  if (expected !== actual) {
    die(`checksum mismatch for ${archiveName}: expected ${expected}, got ${actual}`);
  }
}

function extract(archivePath, outDir) {
  // tar.exe ships on Windows 10 1803+ and handles both .tar.gz and .zip,
  // so a single invocation works across platforms.
  const result = spawnSync("tar", ["-xf", archivePath, "-C", outDir], {
    stdio: "inherit",
  });
  if (result.error) die(`failed to run tar: ${result.error.message}`);
  if (result.status !== 0) die(`tar exited with status ${result.status}`);
}

async function main() {
  if (process.env.LAZYAGENT_SKIP_DOWNLOAD === "1") {
    log("LAZYAGENT_SKIP_DOWNLOAD=1, skipping binary download");
    return;
  }

  const target = detectTarget();
  const { archive, checksums, file } = assetUrl(target);

  fs.mkdirSync(VENDOR_DIR, { recursive: true });
  const finalBinary = path.join(VENDOR_DIR, target.binaryName);
  if (fs.existsSync(finalBinary)) {
    log(`binary already present at ${finalBinary}, skipping download`);
    return;
  }

  const tmpDir = fs.mkdtempSync(path.join(require("node:os").tmpdir(), "lazyagent-"));
  const archivePath = path.join(tmpDir, file);
  const checksumsPath = path.join(tmpDir, "checksums.txt");

  try {
    log(`downloading ${archive}`);
    await download(archive, archivePath);
    log(`downloading ${checksums}`);
    await download(checksums, checksumsPath);
    verifyChecksum(archivePath, checksumsPath, file);

    const extractDir = path.join(tmpDir, "extract");
    fs.mkdirSync(extractDir);
    extract(archivePath, extractDir);

    const extractedBinary = path.join(extractDir, target.binaryName);
    if (!fs.existsSync(extractedBinary)) {
      die(`expected binary ${target.binaryName} not found in archive`);
    }
    fs.copyFileSync(extractedBinary, finalBinary);
    if (target.platform !== "windows") {
      fs.chmodSync(finalBinary, 0o755);
    }
    log(`installed lazyagent to ${finalBinary}`);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
}

main().catch((err) => die(err.message || String(err)));
