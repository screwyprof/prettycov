{
  # PROJECT flake — the toolchain this repo pins, layered ON TOP of the session flake's ambient base.
  # `nix develop` here prepends its PATH, so what this declares wins wherever the two overlap. That
  # ordering is the point: an editor terminal must compile with the same toolchain as the gates.
  description = "prettycov — pretty Go coverage output";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
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
            # Each entry earns its place: everything below is either the toolchain or something a
            # make target invokes by name. bashInteractive is deliberately absent — `nix develop`
            # supplies its own, with readline, whether or not it is listed here.
            packages = [
              pkgs.go_1_27
              pkgs.gopls
              pkgs.gotools
              # Both formats and reports: `make fmt` and `make lint`. Not a go.mod tool — see the
              # note on require-golangci in the Makefile for why.
              pkgs.golangci-lint
              # `make test` pipes `go test -json` through it. Also a go.mod tool, so non-nix users
              # get the same version from `make deps`.
              pkgs.tparse
              # column(1), which `make test-cover-txt` aligns its report with.
              pkgs.util-linux
              # xdg-open: the Linux half of $(OPEN), for `make test-cover-html`.
              pkgs.xdg-utils
              # .pre-commit-config.yaml drives the git hooks; non-nix users bring their own.
              pkgs.pre-commit
              # Every workflow in this repo is a make target.
              pkgs.gnumake
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
          packages.default = pkgs.buildGo127Module rec {
            pname = "prettycov";
            # A flake's `self` exposes rev/shortRev/revCount but NOT tags, so `git describe` is
            # impossible here. ./VERSION is the one thing both nix and the Makefile can read.
            version = pkgs.lib.fileContents ./VERSION;
            src = ./.;
            # Covers the `tool` block's deps too, not just x/tools — bump this whenever go.mod moves.
            vendorHash = "sha256-hzXxg3GKucNEWB7Q/WumrBxtArs+2601jkZH7dnpp3Q=";
            # Without this the version lives only in the derivation name and the binary answers
            # "(devel)": the source has no .git, so the toolchain stamps nothing of its own.
            # No +commit suffix, unlike the Makefile's dev builds — a nix build is pinned to a rev
            # by definition, so the file is the whole story.
            ldflags = [
              "-s"
              "-w"
              "-X main.version=v${version}"
            ];
          };
        };
    };
}
