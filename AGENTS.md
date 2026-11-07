# AGENTS.md

slop is a Go workspace monorepo of terminal apps that share one design language. `apps/<name>` holds each app as its own module, with its private code in `internal/`. `pkg/` holds the shared **kernel**: `utils`, `domain`, `infra` and `tui`, each its own module. `make help` lists the tooling.

**Done** means `make check` is green: gofmt, vet and tests across every module.

## Where code goes

- Write new code in the app's `internal/` first. Promote it to `pkg/` when a second app needs it, and delete the app's copy in the same change.
- **Dependencies point one way:** `utils` ← `domain` ← `infra` / `tui` ← apps. `utils` and `domain` hold pure code, with no I/O or UI. A kernel package is importable on its own, does one thing, and opens with a doc comment saying what that is.
- **New app:** run `make new-app NAME=x`. It wires the `go.mod` replace lines, `go.work` and the `APPS` list in `install.sh`.
- **New kernel module:** it needs the same `require … v0.0.0` plus `replace => ../…` pair in each consumer's `go.mod`, and an entry in `go.work`.
- Before adding, moving or versioning kernel packages, read `pkg/README.md`.

## Building UIs

Declare every screen with `pkg/tui`, and read `pkg/tui/README.md` before writing or changing a view. The rules it doesn't spell out:

- **Colour through Roles** (`tui.Primary`, `tui.Muted`, `tui.Danger`, `tui.Lane(i)`, `tui.Mix(…)`). The palette then re-themes everything, including the scrim behind modals and the live Rex colour sync.
- **Chrome comes from the kernel.** Boxes are `tui.Panel`, modals are `tui.Dialog` via `tui.Modal`/`ZStack`, key help is `tui.KeyHints`. A new kind of chrome is a kernel change, not an app-local one.
- **Hand-drawn content** (gitmon's commit graph and diff lines) goes in a `tui.Func`, painted with `c.P`, the palette passed in at draw time. A palette captured from the model skips the scrim.
- **State lives in the Bubble Tea model.** Views are rebuilt every frame. To animate list scrolling, pass an `anim.Scroller` as the list's `tui.Scroller`.
- **Lip Gloss v2's `Width()` includes the border.** A card that "is 44 wide" is 44 cells outside the border.
- **Bubble Tea v2 key names:** space is `"space"`. Handle `tea.KeyPressMsg`.

## Compatibility contracts

Users already have data on disk, so these values stay fixed:

- gitmon config: `$XDG_CONFIG_HOME/gitmon`
- slash sites: `paths.NativeConfig("slash")/sites.json`
- slash keychain service: `"slash-sftp"`
- the release asset names `<app>_<os>_<arch>.tar.gz` and `checksums.txt`, which `install.sh` downloads

## Runtime conventions

- A TUI owns stdout and stderr. Logging goes through `pkg/infra/logx`, which writes to the state folder when `<APP>_LOG` is set.
- Config files are read and written with `pkg/infra/jsonfile`, secrets with `pkg/infra/keychain`, and folders come from `pkg/infra/paths`.
- Versions come from `pkg/domain/buildinfo`, which the Makefile stamps through ldflags.

## Tests

- Every app has a render test that drives the real model with key presses and asserts each frame is exactly `width × height` cells. Follow the existing ones: `apps/gitmon/internal/ui/screens_test.go` (builds a fixture git repo) and `apps/slash/internal/ui/ui_test.go` (runs an in-process SFTP server).
- Tests that press keys which save settings (`v`, `T`, …) set `XDG_CONFIG_HOME` to `t.TempDir()`, so the developer's real config stays untouched.
- `make screenshots` regenerates `docs/screenshots/*.svg` from those render tests. Run it after visible UI changes.

## Style

- Group imports as standard library, then third party, then `github.com/karim-w/slop/…`.
- Documentation and UI text use plain English: short sentences and concrete words.
- Root `README.md` stays short (screenshots, install, a few commands). Detail lives in each app's `README.md` and in `pkg/*/README.md`.

## Releasing

- **Apps:** a `v*` tag runs `.github/workflows/release.yml`, which publishes every app for macOS and Linux.
- **Kernel modules:** each is tagged on its own as `pkg/<module>/vX.Y.Z`, as described in `pkg/README.md`.
