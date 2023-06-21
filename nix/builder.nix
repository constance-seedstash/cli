{ pkgs }:
let
  namePrefix = "nhost-nix-builder";
  version = "0.0.1";

  commonDeps = with pkgs; [
    cacert
  ];

  # nix-prefetch-docker --image-name nixos/nix --image-tag 2.16.1 --type sha256 --os linux --arch x86_64
  baseimage = {
    x86_64-linux = pkgs.dockerTools.pullImage {
      imageName = "nixos/nix";
      imageDigest = "sha256:04ffc55c794ff2760220f31683d1414f13f963cae397a0686dabcd7564980974";
      finalImageName = "nix";
      finalImageTag = "2.16.1";
      sha256 = "1dncvgi35jy4nwdwbky3vjld4xj30m24x8sfm0w36zbkp07nxqj1";
      os = "linux";
      arch = "x86_64";
    };
    aarch64-linux = pkgs.dockerTools.pullImage {
      imageName = "nixos/nix";
      imageDigest = "sha256:486155b98b4a6bd7dd920b079629bd5ed8c06db37213248d76583b7e7705e023";
      finalImageName = "nix";
      finalImageTag = "2.16.1";
      sha256 = "sha256-JVa+J0XoRO2zrTznVv4BjcqeoGzQg1CVY7Wavs2VMc8=";
      os = "linux";
      arch = "aarch64";
    };
  };
in
rec {
  goDeps = with pkgs; [
    go
  ];

  nodejs18Deps = with pkgs; [
    nodejs-slim
    nodePackages.npm
    nodePackages.pnpm
    yarn
  ] ++ commonDeps;

  image =
    { suffix
    , deps
    }:
    pkgs.dockerTools.buildLayeredImage {
      name = "${namePrefix}-${suffix}";
      tag = version;
      created = "now";

      maxLayers = 125;

      fromImage = baseimage.${pkgs.stdenvNoCC.hostPlatform.system};

      contents = with pkgs; [
        pkgs.stdenv
      ] ++ deps ++ commonDeps;
    };
}
