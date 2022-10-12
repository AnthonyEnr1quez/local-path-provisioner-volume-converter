{ pkgs ? import <nixpkgs> { } }:
with pkgs;

mkShell {
  buildInputs = [
    kube3d
    kubectl
    make
  ];
  shellHook = 
  ''
    zsh
  '';
}
