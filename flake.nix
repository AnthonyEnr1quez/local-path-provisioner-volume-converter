{
  description = "Host to Local PVs";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let pkgs = import nixpkgs { inherit system; };
      in rec {
        packages = flake-utils.lib.flattenTree {
          pv-to-local = let lib = pkgs.lib; in
          pkgs.buildGoModule {
            pname = "pv-to-local";
            version = "0.0.1";

            modSha256 = lib.fakeSha256;
            vendorSha256 = "AIpT5qcP2PGy1JBQyo8DVN0poqC+r9KvtwobgIsWCPI=";

            src = ./.;
          };
        };

        defaultPackage = packages.pv-to-local;
        # defaultApp = packages.pv-to-local;

        devShells = 
        let pkgs = import nixpkgs { inherit system; };
        in rec {
          default = pkgs.mkShell {
            buildInputs = with pkgs; [
              go_1_19
              gotools
              gopls
              go-outline
              gocode
              gopkgs
              gocode-gomod
              godef
              golint

              delve
              glibc

              kube3d
              fluxcd
            ];
          };
        };
      });
}
