# Nix

This directory contains the Nix configuration for the render-mcp-server
development environment. From the repository root, run `direnv allow` or
`nix develop ./nix#dev` to enter the shell.


## Upgrading Nix Packages

### Performing the upgrade

To upgrade nix packages run:
```console
$ nix flake update
```

This will update all inputs.

### Test that things continue to work

1. Open a shell at the root of this repository.
2. Build the environment and perform some tests:
   ```console
   $ nix develop path:nix#dev --command go test ./...
   ```
