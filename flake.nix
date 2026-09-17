{
  # PROJECT flake — a devShell and nothing else, layered ON TOP of the session flake's ambient base.
  #
  # No packages.default on purpose. buildGoModule needs a vendorHash, which is a fixed-output hash
  # nothing can derive from go.sum: it goes stale the moment dependencies move, and no gate here can
  # catch that without running nix in CI. The nixpkgs way to carry a Go program is for nixpkgs to
  # carry it, where a maintainer and r-ryantm own that hash. This repo ships `go install`, and the
  # flake exists so a contributor gets the pinned toolchain — which is what CONTRIBUTING promises.
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
              # 1.26 rather than 1.27: golang/go#80974 splits a straight-line block at blank lines
              # and writes the whole run's statement count into each piece, inflating every number
              # this tool reports. A coverage tool cannot ship on a toolchain that miscounts
              # statements. CL 819000 fixed it for 1.28, with no 1.27 backport.
              pkgs.go_1_26
              pkgs.gopls
              pkgs.gotools
              # No golangci-lint and no vale here, though both gate this repo. The Makefile fetches
              # them with `go run pkg@version` at pinned versions and never probes PATH, so a copy
              # declared here would never be the one that runs — only a second, differently
              # versioned one this flake would then have to keep in step with the Makefile by hand.
              # golangci-lint is the sharper case: `make lint` uses a binary with nilaway compiled
              # in, which a stock one cannot be, so an editor wired to a stock copy reports findings
              # `make lint` does not and misses findings it does.
              #
              # This only stops the flake from adding one. An ambient environment may still put
              # either on PATH — the guarantee is the Makefile's refusal to look there, not this.
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
        };
    };
}
