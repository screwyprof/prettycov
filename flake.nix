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
            packages = [
              pkgs.go_1_26
              pkgs.gopls
              pkgs.gotools
              pkgs.golangci-lint
              pkgs.pre-commit
              pkgs.gnumake
              # No target uses it; `go test -json ./... | tparse` by hand does.
              pkgs.tparse
              # column(1), for test-cover-txt.
              pkgs.util-linux
              # xdg-open, the Linux half of $(OPEN).
              pkgs.xdg-utils
            ];

            # Register the hook on shell entry so nix users never have to remember `make hooks`.
            # Guarded and quiet: entering the shell must not fail because of it.
            shellHook = ''
              if [ -d .git ] && ! grep -q pre-commit .git/hooks/pre-commit 2>/dev/null; then
                pre-commit install >/dev/null 2>&1 || true
              fi
            '';
          };

          # buildGo126Module, not plain buildGoModule: otherwise the shell compiles with 1.26 and the
          # package with the nixpkgs default, which is the toolchain split this pin exists to avoid.
          #
          # 1.26 rather than 1.27: golang/go#80974 splits a straight-line block at blank lines and
          # writes the whole run's statement count into each piece, inflating every number this tool
          # reports. A coverage tool cannot ship on a toolchain that miscounts statements.
          packages.default = pkgs.buildGo126Module rec {
            pname = "prettycov";
            # A flake's `self` exposes rev/shortRev/revCount but NOT tags, so `git describe` is
            # impossible here. ./VERSION is the one thing both nix and the Makefile can read.
            version = pkgs.lib.fileContents ./VERSION;
            src = ./.;
            # Pins the whole module set — bump it whenever go.mod or go.sum moves. `make nix-hash`
            # does that, and the pre-commit hook runs it for anyone with nix.
            vendorHash = "sha256-oyXTsu79HB9wWEKNc4zv2tXj8AcF6yWYtmA/QIrrNW4=";
            # Without this the version lives only in the derivation name and the binary answers
            # "(devel)": the source has no .git, so the toolchain stamps nothing of its own.
            # No +commit suffix, unlike the Makefile's dev builds — a nix build is pinned to a rev
            # by definition, so the file is the whole story.
            ldflags = [
              "-s"
              "-w"
              "-X github.com/screwyprof/prettycov/internal/app.version=v${version}"
            ];
          };
        };
    };
}
