# pkg — the shared kernel

Everything apps share lives here. There are four Go modules. Each holds small packages that you import one at a time.

| Module | Packages | Depends on |
| --- | --- | --- |
| `pkg/utils` | `textx` (ANSI-aware truncate, pad, wrap), `humanize` (bytes, rates, ETAs, ages), `mathx` (clamp, fractions) | the standard library and `x/ansi` |
| `pkg/domain` | `theme` (the colour model, built-in palettes, user themes, terminal-derived themes), `buildinfo` (version stamped at build time) | the standard library |
| `pkg/infra` | `paths` (XDG and OS config, state and cache folders), `jsonfile` (atomic JSON config files), `logx` (file logger for TUIs), `keychain` (OS credential store) | `domain`, `go-keyring` |
| `pkg/tui` | `tui` (declarative views), `anim` (springs), `termsync` (follow the terminal's colours), `themed` (palette-styled Bubbles inputs) | `domain`, `utils`, the Charm v2 stack |

## Rules

- **Dependencies point one way:** `utils` ← `domain` ← `infra` and `tui` ← apps. A package never imports an app, and `utils` and `domain` never import UI or I/O code.
- **Code that only one app needs stays in that app's `internal/`.** Move something into `pkg/` when a second app needs it.
- **Each package does one thing.** Its doc comment says what that is, and an app can import it on its own.

## Using a package

Inside this repo, the `go.work` workspace and the `replace` lines in each app's `go.mod` point at the local copies, so changes show up at once.

From another repo, tag the module and require it:

```sh
git tag pkg/tui/v0.1.0 && git push --tags     # tags are per module: pkg/<module>/vX.Y.Z
go get github.com/karim-w/slop/pkg/tui@v0.1.0
```

When one kernel module needs another (`tui` needs `domain` and `utils`), tag the one it needs first. Then update the `require` version in the dependent module before you tag it.
