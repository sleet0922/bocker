# YAML Incusfile end-to-end projects

These fixtures exercise structured argv execution, explicit shell execution,
host and cross-stage copies, working directories, runtime environment, ports,
domains, entrypoints, commands, and autostart metadata against a real Bocker
daemon.

Run all projects and assertions with:

```bash
BOCKER_BIN=./bocker testdata/yaml-projects/test.sh
```

The script uses dedicated image and container names and removes them on exit.
