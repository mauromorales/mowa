class Mowa < Formula
  desc "MacOS Web API (Mowa) - Interact with MacOS from the web"
  homepage "https://github.com/mauromorales/mowa"
  version "0.3.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/mauromorales/mowa/releases/download/v0.3.0/mowa_Darwin_arm64.zip"
      sha256 "2fd2e6c7eeea6e514b275f3684882b1bb096c1686fa28a1e78f6a8c67cd7853a"
    else
      url "https://github.com/mauromorales/mowa/releases/download/v0.3.0/mowa_Darwin_x86_64.zip"
      sha256 "2da4d209f00dffbc106018f82009847940d03b7943a567ede0759aa5e12a7d2c"
    end
  end

  def install
    bin.install "mowa"
  end

  test do
    system "#{bin}/mowa", "--version"
  end
end 