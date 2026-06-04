# A helper to make a more user friendly shell. Regular Nix shells
# export a ton of unnecessary environment variables. Shells created with
# this helper are much more clean.
#
# This is based off of the following files:
# - https://github.com/numtide/devshell/
#   blob/3e0e60ab37cd0bf7ab59888f5c32499d851edb47/nix/mkNakedShell.nix
# - https://github.com/thenonameguy/devenv/
#   blob/4717da802b1868318ab60758c244c4b37774f426/src/modules/mkNakedShell.nix
#
# Consequently, this code is goverened by the MIT license:
# -----------------------------------------------------------------------------
# MIT License
#
# Copyright (c) 2021 Numtide and contributors
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.
# -----------------------------------------------------------------------------

{ pkgs, system, ... }:
let
  inherit (pkgs) bashInteractive coreutils lib writeTextFile;

  bashPath = "${bashInteractive}/bin/bash";
  stdenv = writeTextFile {
    name = "naked-stdenv";
    destination = "/setup";
    text = ''
      # Fix for `nix develop`
      : ''${outputs:=out}

      runHook() {
        eval "$shellHook"
        unset runHook
      }
    '';
  };
in

{ name ? "nice-shell"
, shellHook ? ""
, envVars ? { }
, profilePackages ? [ ]
, meta ? { }
, passthru ? { }
}:
let

  maybeEscape = value:
    let
      str = toString value;
    in
    if (builtins.match ''".*"'' str != null)
    then str else lib.escapeShellArg str;

  exportEnvVars =
    envVars: builtins.concatStringsSep "\n" (
      lib.mapAttrsToList
        (name: value: "export ${name}=${maybeEscape value}")
        envVars);

  profile = pkgs.buildEnv {
    name = "${name}-profile";
    paths = builtins.concatMap
      (p: builtins.map (o: lib.getOutput o p) p.outputs)
      profilePackages;
    ignoreCollisions = true;
  };

in
(derivation {
  inherit name;
  system = pkgs.stdenv.hostPlatform.system;

  # `nix develop` actually checks and uses builder. And it must be bash.
  builder = bashPath;

  # Bring in the dependencies on `nix-build`
  args = [ "-ec" "${coreutils}/bin/ln -s ${profile} $out; exit 0" ];

  # $stdenv/setup is loaded by nix-shell during startup.
  # https://github.com/nixos/nix/blob/377345e26f1ac4bbc87bb21debcc52a1d03230aa/src/nix-build/nix-build.cc#L429-L432
  inherit stdenv;

  # The shellHook is loaded directly by `nix develop`. But nix-shell
  # requires that other trampoline.
  shellHook = ''
    # Remove all the unnecessary noise that is set by the build env
    unset NIX_BUILD_TOP NIX_BUILD_CORES NIX_STORE
    unset TEMP TEMPDIR TMP TMPDIR
    # $name variable is preserved to keep it compatible with pure shell https://github.com/sindresorhus/pure/blob/47c0c881f0e7cfdb5eaccd335f52ad17b897c060/pure.zsh#L235
    unset builder out shellHook stdenv system
    # Flakes stuff
    unset dontAddDisableDepTrack outputs

    # For `nix develop`. We get /noshell on Linux and /sbin/nologin on macOS.
    if [[ "$SHELL" == "/noshell" || "$SHELL" == "/sbin/nologin" ]]; then
      export SHELL=${bashPath}
    fi

    # add the profile packages to path
    export PATH="${profile}/bin:$PATH"

    # prepend common compilation lookup paths
    export PKG_CONFIG_PATH="${profile}/lib/pkgconfig:$PKG_CONFIG_PATH"
    export LD_LIBRARY_PATH="${profile}/lib:$LD_LIBRARY_PATH"
    export LIBRARY_PATH="${profile}/lib:$LIBRARY_PATH"
    export C_INCLUDE_PATH="${profile}/include:$C_INCLUDE_PATH"

    # shell completions and default data dirs
    export XDG_DATA_DIRS="${profile}/share:$XDG_DATA_DIRS"
    export XDG_CONFIG_DIRS="${profile}/etc/xdg:$XDG_CONFIG_DIRS"

    ${exportEnvVars envVars}

    ${shellHook}
  '';
}) // { inherit meta passthru; } // passthru
