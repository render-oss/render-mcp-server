{ pkgs, ... }:
{
  dev = pkgs.mkNiceShell {
    envVars = {
      RENDER_MCP_SERVER_PATH = ''"$PWD"'';
    };

    profilePackages = with pkgs; [
      # Golang
      go_1_26
      goreleaser

      # Basic unix tools
      coreutils
      findutils
    ];
  };
}
