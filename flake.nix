{
  description = "lazyagent";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          lazyagent = pkgs.buildGoModule {
            pname = "lazyagent";
            version = "unstable-${self.lastModifiedDate or "19700101"}";

            src = self;
            subPackages = [ "cmd/lazyagent" ];

            # Keep this hash in sync with the current Go module graph.
            # If this repo ever checks in a fully populated `vendor/`
            # directory, switch to `vendorHash = null;` instead.
            vendorHash = "sha256-gtIXl8nLaE/BwgqTcESkfJCtLizg2YwxVBeSYfw5E+U=";

            meta = {
              homepage = "https://github.com/chojs23/lazyagent";
              mainProgram = "lazyagent";
              description = "Observe your ai agents and sessions from the terminal";
            };
          };
        in
        {
          default = lazyagent;
          lazyagent = lazyagent;
        });

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/lazyagent";
        };
      });
    };
}
