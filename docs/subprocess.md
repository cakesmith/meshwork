# Local subprocess execution

Use `meshwork execute` with an explicit subprocess configuration and the root that contains the graph and scenario files.

```sh
meshwork execute --scenario meshwork/run.scenario.copy.meshwork.yaml \
  --subprocess meshwork/subprocess.yaml meshwork
```

The configuration connects graph implementation names and local ports to commands and files. For a graph whose `copy` transition uses `files.copy` with ports `source` and `result`, and whose exposed places are `incoming` and `outgoing`, the configuration can be:

```yaml
root: ..
implementations:
  files.copy:
    argv: [sh, "${ROOT}/scripts/copy.sh"]
    inputs: {source: input.txt}
    outputs: {result: output.txt}
inputs: {incoming: data/input.txt}
outputs: {outgoing: data/output.txt}
```

- `root` resolves relative to the configuration file and defaults to its directory. Top-level file paths resolve relative to that root. Place names can be absolute or relative to the scenario's graph namespace.
- `argv` is passed directly to the executable. `${ROOT}` expands to the absolute root. Resolve script paths with `${ROOT}` and select an interpreter through `PATH` or an explicit executable path. Shell syntax is available when the command explicitly invokes a shell.
- Implementation `inputs` and `outputs` bind local transition ports to files inside a fresh working directory for each firing. The linked graph determines the token types.

Meshwork snapshots input files by SHA-256 and checks any matching digest in the scenario's initial marking. After a successful command, Meshwork collects all declared output files before committing the firing. A nonzero exit or a missing output fails the firing. After the scenario succeeds and its expectations pass, Meshwork stages all requested output copies and replaces each destination file individually. It writes JSON with the artifact digests and token hashes to stdout and sends child logs to stderr.

This adapter runs finite real-clock scenarios with memory arcs. The work directory isolates ordinary relative file writes. Run commands from trusted configurations because subprocesses have the caller's OS permissions. External effects fall outside Meshwork's marking transaction.
