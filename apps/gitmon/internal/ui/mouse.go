package ui

import (
	tea "charm.land/bubbletea/v2"
)

func (m *Model) boxAt(x, y int) (box, bool) {
	tr, dr, hr := m.layout()
	switch {
	case tr.has(x, y):
		return boxTree, true
	case dr.has(x, y):
		return boxDiff, true
	case hr.has(x, y):
		return boxGraph, true
	}
	return 0, false
}

func (m *Model) onWheel(msg tea.MouseWheelMsg) tea.Cmd {
	ms := msg.Mouse()
	d := 1
	switch ms.Button {
	case tea.MouseWheelUp, tea.MouseWheelLeft:
		d = -1
	case tea.MouseWheelDown, tea.MouseWheelRight:
	default:
		return nil
	}
	b, ok := m.boxAt(ms.X, ms.Y)
	if !ok {
		return nil
	}
	_, dr, _ := m.layout()
	switch b {
	case boxTree:
		return m.treeSelect(m.ch.sel + d)
	case boxDiff:
		if m.editing {
			return nil
		}
		m.chDiff.scroll(d*3, dr.inner().h)
		m.chDiff.syncHunkToScroll()
		return m.kick()
	case boxGraph:
		// wheel down / right travels toward newer commits
		return m.graphSelect(m.sel - d*3)
	}
	return nil
}

func (m *Model) onClick(msg tea.MouseClickMsg) tea.Cmd {
	ms := msg.Mouse()
	if ms.Button != tea.MouseLeft {
		return nil
	}
	b, ok := m.boxAt(ms.X, ms.Y)
	if !ok {
		return nil
	}
	prev := m.focus
	m.focus = b
	tr, _, hr := m.layout()
	switch b {
	case boxTree:
		in := tr.inner()
		line := m.ch.Offset(in.h, m.treeLines()) + ms.Y - in.y
		for i := 0; i < m.itemCount(); i++ {
			if m.changeLine(i) == line {
				if i == m.ch.sel && prev == boxTree {
					if r, _ := m.selectedRow(); r.kind == rowDir {
						return m.treeKey("enter")
					}
					return m.treeKey("space")
				}
				return m.treeSelect(i)
			}
		}
	case boxGraph:
		g := m.histGeom(hr)
		p := g.xoff + (ms.X-g.in.x-1+g.cw/2)/g.cw
		if p < 0 || p >= g.n {
			return nil
		}
		i := g.n - 1 - p
		if i == m.sel && prev == boxGraph {
			return m.openCommit()
		}
		return m.graphSelect(i)
	}
	return nil
}
