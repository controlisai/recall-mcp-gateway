const os = require("os");
const fs = require("fs");
const path = require("path");
const https = require("https");
const { execSync } = require("child_process");

const VERSION = "1.0.0-beta.2";

const PLATFORM_MAP = { darwin: "darwin", linux: "linux", win32: "windows" };
const ARCH_MAP = { x64: "amd64", arm64: "arm64" };

const platform = PLATFORM_MAP[os.platform()];
const arch = ARCH_MAP[os.arch()];

if (!platform || !arch) {
  console.error(`Unsupported platform: ${os.platform()}-${os.arch()}`);
  process.exit(1);
}

const isWindows = os.platform() === "win32";
const ext = isWindows ? "zip" : "tar.gz";
const binaryName = isWindows ? "recall-mcp-gateway.exe" : "recall-mcp-gateway";
const url = `https://github.com/controlisai/recall-mcp-gateway/releases/download/v${VERSION}/recall-mcp-gateway-${platform}-${arch}.${ext}`;

const binDir = path.join(__dirname, "bin");
const binPath = path.join(binDir, binaryName);

if (fs.existsSync(binPath)) {
  process.exit(0);
}

fs.mkdirSync(binDir, { recursive: true });

const tmpFile = path.join(os.tmpdir(), `recall-mcp-gateway-${VERSION}.${ext}`);

function download(downloadUrl, dest) {
  return new Promise((resolve, reject) => {
    const request = https.get(downloadUrl, (res) => {
      if (res.statusCode === 301 || res.statusCode === 302) {
        return download(res.headers.location, dest).then(resolve).catch(reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`Download failed: HTTP ${res.statusCode}`));
      }
      const file = fs.createWriteStream(dest);
      res.pipe(file);
      file.on("finish", () => {
        file.close();
        resolve();
      });
      file.on("error", reject);
    });
    request.on("error", reject);
  });
}

async function main() {
  console.log(`Downloading recall-mcp-gateway v${VERSION} for ${platform}-${arch}...`);
  await download(url, tmpFile);

  if (isWindows) {
    execSync(
      `powershell -Command "Expand-Archive -Path '${tmpFile}' -DestinationPath '${binDir}' -Force"`,
      { stdio: "inherit" }
    );
    const extracted = path.join(binDir, `recall-mcp-gateway-${platform}-${arch}.exe`);
    if (fs.existsSync(extracted) && !fs.existsSync(binPath)) {
      fs.renameSync(extracted, binPath);
    }
  } else {
    execSync(`tar xzf "${tmpFile}" -C "${binDir}"`, { stdio: "inherit" });
    // The archive contains a platform-suffixed binary — rename to the canonical name
    const extracted = path.join(binDir, `recall-mcp-gateway-${platform}-${arch}`);
    if (fs.existsSync(extracted) && !fs.existsSync(binPath)) {
      fs.renameSync(extracted, binPath);
    }
    fs.chmodSync(binPath, 0o755);
  }

  fs.unlinkSync(tmpFile);
  console.log(`Installed recall-mcp-gateway to ${binPath}`);
}

main().catch((err) => {
  console.error(`Failed to install recall-mcp-gateway: ${err.message}`);
  console.error(
    "You can download manually from: https://github.com/controlisai/recall-mcp-gateway/releases"
  );
  process.exit(1);
});
