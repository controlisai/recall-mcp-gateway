# Homebrew formula for Recall MCP Gateway
# Tap: controlisai/tap
# Install: brew install controlisai/tap/recall-mcp-gateway
#
# To publish:
# 1. Build release binaries: make release
# 2. Create GitHub release and upload artifacts
# 3. Update SHA256 values below
# 4. Push to homebrew-tap repo

class RecallMcpGateway < Formula
  desc "MCP tool monitoring & governance gateway for AI agents"
  homepage "https://github.com/controlisai/recall-mcp-gateway"
  version "1.0.0-beta.2"
  license "AGPL-3.0-only"

  on_macos do
    on_arm do
      url "https://github.com/controlisai/recall-mcp-gateway/releases/download/v1.0.0-beta.2/recall-mcp-gateway-darwin-arm64.tar.gz"
      sha256 "8357605d81eef8c75b3caec35a805480a92c570311d86631c08dde6a671d2620"
    end
    on_intel do
      url "https://github.com/controlisai/recall-mcp-gateway/releases/download/v1.0.0-beta.2/recall-mcp-gateway-darwin-amd64.tar.gz"
      sha256 "d0d95cd3b6edd7cc60fa494cb49cefdf687885b8e961d00281ecfec53d7c0aa1"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/controlisai/recall-mcp-gateway/releases/download/v1.0.0-beta.2/recall-mcp-gateway-linux-arm64.tar.gz"
      sha256 "5041b496593da1b1ce63f4ed970ca726c026d5f4f909807c40947ecd8bebb22e"
    end
    on_intel do
      url "https://github.com/controlisai/recall-mcp-gateway/releases/download/v1.0.0-beta.2/recall-mcp-gateway-linux-amd64.tar.gz"
      sha256 "53bf775517f95399033ff6bfa19b7208bcf16711506078155a8107bd2bfa0965"
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
