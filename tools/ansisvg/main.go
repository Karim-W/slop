// Command ansisvg turns a captured terminal frame (text with ANSI colours)
// into an SVG "screenshot" with window chrome, for READMEs.
//
//	ansisvg -bg '#0d0e1a' -fg '#e6e6fa' -title gitmon < frame.ansi > shot.svg
package main

import (
	"bufio"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	cellW   = 8.4
	cellH   = 18.0
	font    = 14.0
	padX    = 16.0
	padTop  = 40.0
	padBot  = 16.0
	radius  = 10.0
	fontCSS = `ui-monospace, "SF Mono", "JetBrains Mono", Menlo, Consolas, monospace`
)

type style struct {
	fg, bg                       string
	bold, faint, italic, underln bool
	reverse                      bool
}

type run struct {
	text  string
	cells int
	st    style
}

func main() {
	bg := flag.String("bg", "#0d0e1a", "default background")
	fg := flag.String("fg", "#e6e6fa", "default foreground")
	title := flag.String("title", "", "window title")
	flag.Parse()

	data, err := io.ReadAll(bufio.NewReader(os.Stdin))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	var rows [][]run
	width := 0
	var st style
	for _, l := range lines {
		r := parse(l, &st)
		n := 0
		for _, x := range r {
			n += x.cells
		}
		width = max(width, n)
		rows = append(rows, r)
	}
	w := float64(width)*cellW + 2*padX
	h := float64(len(rows))*cellH + padTop + padBot

	out := &strings.Builder{}
	fmt.Fprintf(out, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f">`+"\n", w, h, w, h)
	fmt.Fprintf(out, `<rect width="100%%" height="100%%" rx="%.0f" fill="%s"/>`+"\n", radius, *bg)
	for i, c := range []string{"#ff5f57", "#febc2e", "#28c840"} {
		fmt.Fprintf(out, `<circle cx="%.0f" cy="20" r="6" fill="%s"/>`+"\n", padX+6+float64(i)*20, c)
	}
	if *title != "" {
		fmt.Fprintf(out, `<text x="%.0f" y="24" fill="%s" fill-opacity="0.6" font-family='%s' font-size="12" text-anchor="middle">%s</text>`+"\n",
			w/2, *fg, fontCSS, html.EscapeString(*title))
	}
	fmt.Fprintf(out, `<g font-family='%s' font-size="%.0f" xml:space="preserve">`+"\n", fontCSS, font)
	for y, r := range rows {
		x := 0
		top := padTop + float64(y)*cellH
		for _, seg := range r {
			f, b := seg.st.fg, seg.st.bg
			if seg.st.reverse {
				f, b = b, f
				if f == "" {
					f = *bg
				}
				if b == "" {
					b = *fg
				}
			}
			if f == "" {
				f = *fg
			}
			px := padX + float64(x)*cellW
			if b != "" {
				fmt.Fprintf(out, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`+"\n",
					px, top, float64(seg.cells)*cellW+0.5, cellH+0.5, b)
			}
			if strings.TrimSpace(seg.text) != "" {
				attrs := ""
				if seg.st.bold {
					attrs += ` font-weight="bold"`
				}
				if seg.st.italic {
					attrs += ` font-style="italic"`
				}
				if seg.st.faint {
					attrs += ` fill-opacity="0.55"`
				}
				if seg.st.underln {
					attrs += ` text-decoration="underline"`
				}
				fmt.Fprintf(out, `<text x="%.1f" y="%.1f" fill="%s" textLength="%.1f" lengthAdjust="spacing"%s>%s</text>`+"\n",
					px, top+cellH*0.75, f, float64(seg.cells)*cellW, attrs, strings.ReplaceAll(html.EscapeString(seg.text), " ", "\u00a0"))
			}
			x += seg.cells
		}
	}
	out.WriteString("</g>\n</svg>\n")
	os.Stdout.WriteString(out.String())
}

// parse splits one line into styled runs, carrying style across lines.
func parse(line string, st *style) []run {
	var out []run
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, run{cur.String(), ansi.StringWidth(cur.String()), *st})
			cur.Reset()
		}
	}
	for i := 0; i < len(line); {
		if line[i] != 0x1b {
			j := i
			for j < len(line) && line[j] != 0x1b {
				j++
			}
			// split per grapheme width so wide runes keep their cells
			cur.WriteString(line[i:j])
			i = j
			continue
		}
		// CSI ... final byte
		if i+1 < len(line) && line[i+1] == '[' {
			j := i + 2
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}
			if j < len(line) && line[j] == 'm' {
				flush()
				sgr(line[i+2:j], st)
			}
			i = j + 1
			continue
		}
		// OSC ... ST/BEL: skip
		if i+1 < len(line) && line[i+1] == ']' {
			j := i + 2
			for j < len(line) && line[j] != 0x07 && !(line[j] == 0x1b && j+1 < len(line) && line[j+1] == '\\') {
				j++
			}
			if j < len(line) && line[j] == 0x1b {
				j++
			}
			i = j + 1
			continue
		}
		i += 2
	}
	flush()
	return out
}

var basic = []string{"#000000", "#cd3131", "#0dbc79", "#e5e510", "#2472c8", "#bc3fbc", "#11a8cd", "#e5e5e5",
	"#666666", "#f14c4c", "#23d18b", "#f5f543", "#3b8eea", "#d670d6", "#29b8db", "#ffffff"}

func sgr(params string, st *style) {
	if params == "" {
		*st = style{}
		return
	}
	p := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	num := func(i int) int {
		if i >= len(p) {
			return 0
		}
		n, _ := strconv.Atoi(p[i])
		return n
	}
	for i := 0; i < len(p); i++ {
		n := num(i)
		switch {
		case n == 0:
			*st = style{}
		case n == 1:
			st.bold = true
		case n == 2:
			st.faint = true
		case n == 3:
			st.italic = true
		case n == 4:
			st.underln = true
		case n == 7:
			st.reverse = true
		case n == 22:
			st.bold, st.faint = false, false
		case n == 23:
			st.italic = false
		case n == 24:
			st.underln = false
		case n == 27:
			st.reverse = false
		case n == 39:
			st.fg = ""
		case n == 49:
			st.bg = ""
		case n >= 30 && n <= 37:
			st.fg = basic[n-30]
		case n >= 90 && n <= 97:
			st.fg = basic[n-90+8]
		case n >= 40 && n <= 47:
			st.bg = basic[n-40]
		case n >= 100 && n <= 107:
			st.bg = basic[n-100+8]
		case (n == 38 || n == 48) && num(i+1) == 2:
			c := fmt.Sprintf("#%02x%02x%02x", num(i+2), num(i+3), num(i+4))
			if n == 38 {
				st.fg = c
			} else {
				st.bg = c
			}
			i += 4
		case (n == 38 || n == 48) && num(i+1) == 5:
			c := xterm256(num(i + 2))
			if n == 38 {
				st.fg = c
			} else {
				st.bg = c
			}
			i += 2
		}
	}
}

func xterm256(n int) string {
	switch {
	case n < 16:
		return basic[n]
	case n < 232:
		n -= 16
		lv := []int{0, 95, 135, 175, 215, 255}
		return fmt.Sprintf("#%02x%02x%02x", lv[n/36], lv[n/6%6], lv[n%6])
	}
	g := 8 + (n-232)*10
	return fmt.Sprintf("#%02x%02x%02x", g, g, g)
}
