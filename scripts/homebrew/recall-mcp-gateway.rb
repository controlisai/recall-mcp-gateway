# Homebrew formula for Recall MCP Gateway
# Tap: controlisai/tap
# Install: brew install controlisai/tap/recall-mcp-gateway
#
# To publish:
# 1. Build release binaries: make build-all package checksums
# 2. Upload to releases.controlis.ai
# 3. Update SHA256 values below
# 4. Push to homebrew-tap repo

class RecallGateway < Formula
  desc "MCP tool monitoring & governance gateway for AI agents"
  homepage "https://controlis.ai"
  version "VERSION_PLACEHOLDER"
  license "AGPL-3.0-only"

  on_macos do
    on_arm do
      url "https://releases.controlis.ai/recall-mcp-gateway/vVERSION_PLACEHOLDER/recall-mcp-gateway-darwin-arm64.tar.gz"
      sha256 "SHA256_DARWIN_ARM64"
    end
    on_intel do
      url "https://releases.controlis.ai/recall-mcp-gateway/vVERSION_PLACEHOLDER/recall-mcp-gateway-darwin-amd64.tar.gz"
      sha256 "SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    on_arm do
      url "https://releases.controlis.ai/recall-mcp-gateway/vVERSION_PLACEHOLDER/recall-mcp-gateway-linux-arm64.tar.gz"
      sha256 "SHA256_LINUX_ARM64"
    end
    on_intel do
      url "https://releases.controlis.ai/recall-mcp-gateway/vVERSION_PLACEHOLDER/recall-mcp-gateway-linux-amd64.tar.gz"
      sha256 "SHA256_LINUX_AMD64"
    end
  end

  def install
    bin.install "recall-mcp-gateway"
  end

  def post_install
    ohai "Recall MCP Gateway installed!"
    ohai "Quick start: recall-mcp-gateway --from-claude-config"
    ohai "Dashboard:   http://localhost:8090/dashboard"
    ohai "Docs:        https://controlis.ai/#/docs/deployment"
  end

  test do
    output = shell_output("#{bin}/recall-mcp-gateway --version")
    assert_match "recall-mcp-gateway", output
  end

  service do
    run [opt_bin/"recall-mcp-gateway", "--config", etc/"recall-mcp-gateway/config.yaml"]
    keep_alive true
    log_path var/"log/recall-mcp-gateway.log"
    error_log_path var/"log/recall-mcp-gateway-error.log"
  end
end
