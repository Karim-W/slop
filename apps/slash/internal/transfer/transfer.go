// Package transfer moves files between the local disk and the server using a
// pool of SFTP connections. Large files are split into chunks so that a single
// big download can use every connection at once.
package transfer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type Direction int

const (
	Upload Direction = iota
	Download
)

type State int

const (
	Queued State = iota
	Running
	Done
	Failed
	Canceled
)

func (s State) Finished() bool {
	return s == Done || s == Failed || s == Canceled
}

// Transfer is one file being copied. Its chunks may run on several
// connections at the same time, so all mutable state is guarded.
type Transfer struct {
	ID        int
	Direction Direction
	Name      string
	Src       string
	Dst       string
	Size      int64
	ModTime   time.Time

	ctx    context.Context
	cancel context.CancelFunc

	transferred atomic.Int64

	prepareOnce        sync.Once
	prepareErr         error
	createdDestination bool

	mu            sync.Mutex
	state         State
	err           error
	pendingChunks int
	startedAt     time.Time
	finishedAt    time.Time
}

func newTransfer(id int, dir Direction, name, src, dst string, size int64, modTime time.Time) *Transfer {
	ctx, cancel := context.WithCancel(context.Background())
	return &Transfer{
		ID: id, Direction: dir, Name: name, Src: src, Dst: dst,
		Size: size, ModTime: modTime, ctx: ctx, cancel: cancel,
	}
}

func failedPlan(id int, dir Direction, name string, err error) *Transfer {
	t := newTransfer(id, dir, name, "", "", 0, time.Time{})
	t.state = Failed
	t.err = err
	t.finishedAt = time.Now()
	return t
}

func (t *Transfer) markRunning() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state == Queued {
		t.state = Running
		t.startedAt = time.Now()
	}
}

// finishChunk records the outcome of one chunk and reports whether it was the
// last one, along with the transfer's resulting state.
func (t *Transfer) finishChunk(err error) (last bool, state State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pendingChunks--
	if err != nil && !t.state.Finished() {
		t.state = Failed
		t.err = err
		t.cancel()
	}
	if t.pendingChunks > 0 {
		return false, t.state
	}
	if t.state == Running || t.state == Queued {
		t.state = Done
	}
	t.finishedAt = time.Now()
	return true, t.state
}

func (t *Transfer) markCanceled() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state.Finished() {
		return
	}
	t.state = Canceled
	t.err = context.Canceled
	t.cancel()
}

func (t *Transfer) view() View {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := View{
		ID:          t.ID,
		Direction:   t.Direction,
		Name:        t.Name,
		Size:        t.Size,
		Transferred: t.transferred.Load(),
		State:       t.state,
	}
	if t.err != nil && !errors.Is(t.err, context.Canceled) {
		v.Err = t.err
	}
	if !t.startedAt.IsZero() {
		end := t.finishedAt
		if end.IsZero() {
			end = time.Now()
		}
		if elapsed := end.Sub(t.startedAt).Seconds(); elapsed > 0 {
			v.BytesPerSec = float64(v.Transferred) / elapsed
		}
	}
	if v.State == Running && v.BytesPerSec > 0 {
		remaining := float64(v.Size - v.Transferred)
		v.ETA = time.Duration(remaining / v.BytesPerSec * float64(time.Second))
	}
	return v
}

// View is an immutable snapshot of a Transfer, safe to hand to the UI.
type View struct {
	ID          int
	Direction   Direction
	Name        string
	Size        int64
	Transferred int64
	State       State
	Err         error
	BytesPerSec float64
	ETA         time.Duration
}

func (v View) Progress() float64 {
	if v.Size == 0 {
		if v.State == Done {
			return 1
		}
		return 0
	}
	return float64(v.Transferred) / float64(v.Size)
}
