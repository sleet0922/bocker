# Bocker GUI

Ubuntu Linux desktop application for Bocker, built with Flutter and Material 3.
It is a separate frontend: it invokes the existing `bocker` command and does
not change the CLI or its state format.

## Architecture

- `bocker` is the container engine and CLI. It owns the embedded Incus daemon,
  `/var/lib/bocker` state, networking, images, and all container operations. It
  can be installed and used independently without the GUI.
- `bocker_gui` is an unprivileged Flutter frontend. It contains no container
  engine and sends the same CLI arguments to the matching bundled `bocker`.
- On first use, the GUI asks PolicyKit to start a restricted root helper. Later
  actions use a user-private Unix socket, including the interactive container
  shell, so the GUI itself remains a normal desktop process.

The release bundle keeps `bocker_gui`, `bocker`, `lib/`, and `data/` together.
The CLI can still be copied to `/usr/local/bin/bocker` for independent terminal
use; the bundled GUI deliberately uses its adjacent copy to avoid version
mismatches.

## Run in development

From this directory:

```bash
flutter run -d linux
```

The application uses the matching `bocker` executable beside `bocker_gui`. It
requests authorization once, then starts a root-owned local helper for the
current desktop session. Later GUI operations use that helper, so they do not
prompt again.

For an uninstalled development binary, set its path:

```bash
BOCKER_BINARY="$PWD/../bocker" flutter run -d linux
```

The desktop session needs a PolicyKit authentication agent, which is included
by default on standard Ubuntu desktop installations.

## Build a release

```bash
./build_release.sh
```

Both `bocker_gui` and its matching `bocker` CLI are written to
`build/linux/x64/release/bundle/`. Keep the bundle contents together when moving
or packaging the application.
