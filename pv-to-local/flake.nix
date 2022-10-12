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
            vendorSha256 = null;

            src = ./.;
          };

          pvm =
          pkgs.stdenv.mkDerivation {
            name = "pvm";

            version = "1.0.0";

            # https://nixos.wiki/wiki/Packaging/Binaries
            src = pkgs.fetchurl {
                url = https://github.com/utkuozdemir/pv-migrate/releases/download/v1.0.0/pv-migrate_v1.0.0_linux_x86_64.tar.gz;
                sha256 = "yrspp8coVIiEZ+YPToq+ksC+pl9aaiD0WNTgHOw5tWE=";
            };

            sourceRoot = ".";

            installPhase = ''
                mkdir -p $out/bin
                cp pv-migrate $out/bin/pv-migrate
                chmod +x $out/bin/pv-migrate
            '';
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
            ];
          };
        };
      });
}
