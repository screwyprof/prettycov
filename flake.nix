{
  # PROJECT flake — a devShell and nothing else, layered ON TOP of the session flake's ambient base.
  #
  # `nix develop` here prepends its PATH, so what this declares wins wherever the two overlap. That
  # ordering is the point: an editor terminal must compile with the same toolchain as the gates.
  #
  # Adding a packages.default brings back a vendorHash, which nothing can derive from go.sum and no
  # gate here can check without nix in CI. `go install` is how this ships.
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
