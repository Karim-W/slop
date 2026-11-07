# tui

A small declarative UI kit for terminal apps, built on Bubble Tea v2 and Lip Gloss v2 and modelled on SwiftUI and Flutter. You describe the screen as a tree of views built from your model, and `tui` lays it out and draws it.

```go
func (m *Model) View() tea.View {
	root := tui.VStack(
		tui.HStack(tui.Gradient(" ◆ myapp ").Bold(), tui.Spacer(), tui.Text(m.status).Fg(tui.Muted)),
		tui.HStack(
			tui.Panel(m.fileList()).Title("Files").Focused(m.focus == 0).Grow(),
			tui.Panel(m.preview()).Title("Preview").Focused(m.focus == 1).Grow(),
		).Grow(),
		tui.KeyHints(tui.H("tab", "switch"), tui.H("q", "quit")),
	)
	var dialog tui.View
	if m.confirming {
		dialog = tui.Dialog("Delete?", tui.Text("This cannot be undone.")).Danger(true)
	}
	return tea.NewView(tui.Render(m.palette, tui.Modal(root, dialog), m.width, m.height))
}
```

State lives in your Bubble Tea model. Views are plain values that you rebuild every frame, so they never go out of sync with the model.

## How layout works

Layout works like Flutter's:

1. A parent offers each child a size (`Measure(ctx, avail)`), and the child answers with the size it wants within that.
2. The parent decides on the final sizes and calls `Draw(ctx, size)`. The child must return exactly `size.H` lines of exactly `size.W` cells.

`VStack` and `HStack` give fixed children the size they ask for. Children marked `Grow` (and `Spacer`) share what is left in proportion to their weight. Across the stack, every child is stretched to the full width (VStack) or height (HStack). Nil children are skipped, so `tui.If(cond, view)` works inline.

## The design language

Views name semantic **roles** instead of raw colours:

| Role | Use |
| --- | --- |
| `Primary`, `Secondary`, `Muted` | body text, supporting text, hints |
| `Accent`, `Accent2`, `Accent3` | brand colour, focus, gradients |
| `Success`, `Warning`, `Danger`, `Info` | status |
| `Background`, `Surface`, `Overlay`, `Border`, `Selection` | structure |
| `Added`, `Removed`, `Modified` | diffs and changes |

A `*tui.Palette` (made with `tui.NewPalette(theme)`) resolves roles to colours when drawing. Swap the palette and the whole app re-themes. The themes themselves live in `pkg/domain/theme`, so every app gets the same built-in themes: nebula, catppuccin, tokyo-night, rose-pine and more. With `termsync`, an app can also follow the terminal's own colours live, as it does in Rex.

`tui.Lane(i)` is the i-th categorical colour, for telling many things apart (graph lanes, branches). `tui.Mix(Background, Accent, 0.15)` blends two roles. `tui.Hex("#ff00ff")` is a fixed colour, for the rare case that needs one.

## Views

| View | What it does |
| --- | --- |
| `Text(s)`, `Textf`, `Raw(ansi)` | Text. Modifiers: `.Fg .Bg .Bold .Italic .Faint .Wrap .Align`. Use `Raw` for output from other components, such as a Bubbles input. |
| `Line(S("a", style), …)` | One line made of differently styled spans. |
| `VStack`, `HStack` | Columns and rows. `.Gap(n)` and `.Grow()`. |
| `Spacer()`, `Grow(w, v)` | Flexible space, and flexible views. |
| `Pad(v, …)`, `Width`, `Height`, `Frame` | Padding and fixed sizes. |
| `Center(v)`, `Align(h, v, child)` | Place a child inside the space it gets. |
| `Tint(color, v)` | A solid background that survives nested style resets. |
| `Panel(body)` | The standard box: title, subtitle, right label, key hints and a gradient border when focused. `.Body(v)` swaps the content. |
| `Dialog(title, body…)` | The standard modal card. `.Danger(true)` for destructive actions. |
| `List(n, cursor, scroller, row)` | A virtualised, scrolling list. `.Highlight(Selection)` and `.Empty(view)`. Pass `&tui.Scroll{}` for instant scrolling or an `*anim.Scroller` for spring-animated scrolling. |
| `ZStack(base, layers…)`, `Modal(base, dialog)` | Float views over a base. `Modal` fades the base with the palette, not by stripping colour. |
| `KeyHints(H("q", "quit"), …)` | Key help that drops hints from the end when space runs out. |
| `ProgressBar(f)`, `Gradient(text)`, `Chip`, `Divider`, `VDivider`, `Checkbox` | Small widgets. |
| `ForEach(items, fn)`, `Group(…)`, `If`, `IfElse` | Composition helpers. |
| `Lazy(func(size) View)`, `Func(func(ctx, size) []string)` | Build a subtree once its size is known, or draw something by hand. |

## Writing your own view

Implement `View`:

```go
type Meter struct{ Value float64 }

func (m Meter) Measure(_ tui.Ctx, avail tui.Size) tui.Size { return tui.Size{W: avail.W, H: 1} }

func (m Meter) Draw(c tui.Ctx, s tui.Size) []string {
	return tui.Draw(c, tui.HStack(tui.Text("cpu "), tui.ProgressBar(m.Value).Color(tui.Warning)), s)
}
```

To make your view stretch in a stack, wrap it in `tui.Grow(1, view)`.

For content you draw cell by cell, such as gitmon's commit graph and diff lines, use `tui.Func`. Paint with `c.P`, the palette passed in at draw time. Then theme changes and modal scrims apply to it as well.

## Subpackages

- **`anim`**: spring presets (`Scroll`, `Panel`, `Pop`), `Spring`, `Scroller` (a `tui.Scroller`) and the `Frame()` tick.
- **`termsync`**: asks the terminal for its colours (`Query`, `ParseOSC4`) and provides `Gate`. While colour replies are in flight, `Gate` holds back quitting and handing the terminal to an editor, so the replies never land in your shell.
- **`themed`**: Bubbles text inputs and spinners styled from a palette.
