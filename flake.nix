{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let pkgs = import nixpkgs { inherit system; };
      in rec
      {
        packages = flake-utils.lib.flattenTree {
          local-path-provisioner-volume-converter = pkgs.buildGoModule {
            pname = "local-path-provisioner-volume-converter";
            version = "0.0.5";

            modSha256 = pkgs.lib.fakeSha256;
            vendorSha256 = "VWNUd+McY0V+X96+4ClVCwOyMD94ogVdvBuQb4VeE/I=";

            CGO_ENABLED = 0;

            src = ./.;
            checkPhase = ""; #todo
          };
        };

        defaultPackage = packages.local-path-provisioner-volume-converter;

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go_1_20
            gotools
            gopls
            go-outline
            gocode
            gopkgs
            gocode-gomod
            godef
            golint
            delve
            kube3d
            fluxcd
          ];
        };
      });
}
