# flake.nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/25.05"; # Or a specific release branch
    # You may need to add other inputs for specific Go-related tools
  };

  outputs =
    { self, nixpkgs, ... }@inputs:
    let
      system = "x86_64-linux"; # Replace with your system architecture
      pkgs = import nixpkgs {
        inherit system;
      };
    in
    {
      devShells.${system}.default = pkgs.mkShell {

        buildInputs = with pkgs; [
	  go
	  gopls
        ];

        # Set environment variables if needed (e.g., GOPROXY)
        shellHook = ''
          	 export PATH=$PATH:''${GOPATH:-~/.local/share/go}/bin
        '';
      };
    };
}
