{
  # PROJECT flake — the toolchain this repo pins, layered ON TOP of the session flake's ambient base.
  # `nix develop` here prepends its PATH, so what this declares wins wherever the two overlap. That
  # ordering is the point: an editor terminal must compile with the same toolchain as the gates.
  description = "prettycov — pretty Go coverage output";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      # FOLLOW ours, or flake-parts drags in a SECOND nixpkgs-lib: two evaluations of anything shared.
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };
  };

  outputs =
    inputs@{ flake-parts, ... }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      # The two systems this is actually built on — the cage and the operator's Mac. Not a
      # default-systems sweep: every extra entry is eval work for a platform nobody builds here.
      systems = [
        "aarch64-linux"
        "aarch64-darwin"
      ];

      perSystem =
        { pkgs, ... }:
        {
          devShells.default = pkgs.mkShell {
            packages = [
              pkgs.go_1_27
              pkgs.gopls
              pkgs.gotools
              pkgs.golangci-lint
              pkgs.gofumpt
              pkgs.gci
              pkgs.tparse
              pkgs.go-cover-treemap
              pkgs.util-linux
              pkgs.xdg-utils
              # .pre-commit-config.yaml drives the git hooks; non-nix users bring their own.
              pkgs.pre-commit
              # `make` targets shell out to these: VERSION uses git describe, and SHELL := bash.
              pkgs.gnumake
              pkgs.bashInteractive
            ];

            # Register the hook on shell entry so nix users never have to remember `make hooks`.
            # Guarded and quiet: entering the shell must not fail because of it.
            shellHook = ''
              if [ -d .git ] && ! grep -q pre-commit .git/hooks/pre-commit 2>/dev/null; then
                pre-commit install >/dev/null 2>&1 || true
              fi
            '';
          };

          # buildGo127Module, not plain buildGoModule: otherwise the shell compiles with 1.27 and the
          # package with the nixpkgs default, which is the toolchain split this pin exists to avoid.
          packages.default = pkgs.buildGo127Module {
            pname = "prettycov";
            version = "0.0.1-alpha";
            src = ./.;
            # Covers the `tool` block's deps too, not just x/tools — bump this whenever go.mod moves.
            vendorHash = "sha256-9zihyHQtL4beMcUUutAsfTHmc5OAgo2bYk0vQODPJCo=";
          };
        };
    };
}
