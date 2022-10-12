{ pkgs ? import <nixpkgs> {} }:
pkgs.stdenv.mkDerivation rec {
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

      }
# pkgs.stdenv.mkDerivation {
#   name = "pv-migrate";
#   src = pkgs.fetchurl {
#     url = "https://github.com/utkuozdemir/pv-migrate/releases/download/v1.0.0/pv-migrate_v1.0.0_linux_x86_64.tar.gz";
#     sha256 = "yrspp8coVIiEZ+YPToq+ksC+pl9aaiD0WNTgHOw5tWE=";
#   };
#   phases = ["installPhase" "patchPhase"];
#   installPhase = ''
#     mkdir -p $out/bin
#     cp $src $out/bin/pv-migrate
#     chmod +x $out/bin/pv-migrate
#   '';
