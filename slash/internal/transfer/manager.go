package transfer

import (
	"errors"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/karim-w/slop/slash/internal/vfs"
)

const (
	chunkSize  = 32 << 20
	bufferSize = 1 << 20
)

type Request struct {
	Direction Direction
	Sources   []string
	DestDir   string
}

type Stats struct {
	Transfers        []View
	BytesTransferred int64
	// Completed only ever increases, so the UI can cheaply notice that new
	// files landed and refresh its listings.
	Completed uint64
}

type Manager struct {
	local   vfs.FileSystem
	remote  vfs.FileSystem
	workers []vfs.FileSystem

	queue *chunkQueue
	wg    sync.WaitGroup

	mu        sync.Mutex
	transfers []*Transfer
	nextID    int

	bytesTransferred atomic.Int64
	completed        atomic.Uint64
}

// NewManager starts one worker per remote connection. remote is used for
// planning (walking directories, creating folders) and workers do the copying.
func NewManager(local, remote vfs.FileSystem, workers []vfs.FileSystem) *Manager {
	m := &Manager{local: local, remote: remote, workers: workers, queue: newChunkQueue()}
	for _, worker := range workers {
		m.wg.Add(1)
		go m.runWorker(worker)
	}
	return m
}

func (m *Manager) Connections() int { return len(m.workers) }

func (m *Manager) Close() {
	m.CancelAll()
	m.queue.close()
	m.wg.Wait()
}

func (m *Manager) Enqueue(req Request) {
	go m.plan(req)
}

func (m *Manager) Stats() Stats {
	m.mu.Lock()
	transfers := slices.Clone(m.transfers)
	m.mu.Unlock()

	views := make([]View, len(transfers))
	for i, t := range transfers {
		views[i] = t.view()
	}
	return Stats{
		Transfers:        views,
		BytesTransferred: m.bytesTransferred.Load(),
		Completed:        m.completed.Load(),
	}
}

func (m *Manager) Cancel(id int) {
	if t := m.find(id); t != nil {
		t.markCanceled()
	}
}

func (m *Manager) CancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.transfers {
		t.markCanceled()
	}
}

// Retry re-queues a failed or canceled file as a fresh transfer.
func (m *Manager) Retry(id int) {
	old := m.find(id)
	if old == nil || old.Src == "" {
		return
	}
	if state := old.view().State; state != Failed && state != Canceled {
		return
	}
	m.remove(id)
	m.add(old.Direction, old.Name, old.Src, old.Dst, old.Size, old.ModTime)
}

func (m *Manager) ClearFinished() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transfers = slices.DeleteFunc(m.transfers, func(t *Transfer) bool {
		return t.view().State.Finished()
	})
}

func (m *Manager) plan(req Request) {
	src, dst := m.sides(req.Direction, m.remote)
	for _, source := range req.Sources {
		base := src.Parent(source)
		err := vfs.Walk(src, source, func(path string, entry vfs.Entry) error {
			relative := relativeTo(src, base, path)
			target := dst.Join(req.DestDir, relative)
			if entry.IsDir {
				return dst.MkdirAll(target)
			}
			m.add(req.Direction, relative, path, target, entry.Size, entry.ModTime)
			return nil
		})
		if err != nil {
			m.mu.Lock()
			m.nextID++
			m.transfers = append(m.transfers, failedPlan(m.nextID, req.Direction, src.Base(source), err))
			m.mu.Unlock()
		}
	}
}

func (m *Manager) add(dir Direction, name, src, dst string, size int64, modTime time.Time) {
	m.mu.Lock()
	m.nextID++
	t := newTransfer(m.nextID, dir, name, src, dst, size, modTime)
	chunks := splitIntoChunks(t)
	t.pendingChunks = len(chunks)
	m.transfers = append(m.transfers, t)
	m.mu.Unlock()
	m.queue.push(chunks...)
}

func splitIntoChunks(t *Transfer) []chunk {
	if t.Size <= chunkSize {
		return []chunk{{transfer: t, offset: 0, length: t.Size}}
	}
	var chunks []chunk
	for offset := int64(0); offset < t.Size; offset += chunkSize {
		chunks = append(chunks, chunk{transfer: t, offset: offset, length: min(chunkSize, t.Size-offset)})
	}
	return chunks
}

func (m *Manager) runWorker(remote vfs.FileSystem) {
	defer m.wg.Done()
	for {
		next, ok := m.queue.pop()
		if !ok {
			return
		}
		m.copyChunk(next, remote)
	}
}

func (m *Manager) copyChunk(c chunk, remote vfs.FileSystem) {
	t := c.transfer
	src, dst := m.sides(t.Direction, remote)

	err := t.ctx.Err()
	if err == nil {
		t.markRunning()
		err = m.prepareDestination(t, dst)
	}
	if err == nil {
		err = m.copyRange(t, src, dst, c.offset, c.length)
	}

	last, state := t.finishChunk(err)
	if !last {
		return
	}
	switch state {
	case Done:
		_ = dst.SetModTime(t.Dst, t.ModTime)
		m.completed.Add(1)
	case Failed, Canceled:
		if t.createdDestination {
			_ = dst.RemoveAll(t.Dst)
		}
	}
}

// prepareDestination creates the destination file exactly once per transfer,
// no matter how many chunks race to start it.
func (m *Manager) prepareDestination(t *Transfer, dst vfs.FileSystem) error {
	t.prepareOnce.Do(func() {
		t.prepareErr = dst.CreateSized(t.Dst, t.Size)
		t.createdDestination = t.prepareErr == nil
	})
	return t.prepareErr
}

func (m *Manager) copyRange(t *Transfer, src, dst vfs.FileSystem, offset, length int64) error {
	if length == 0 {
		return nil
	}
	in, err := src.OpenRead(t.Src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := dst.OpenWrite(t.Dst)
	if err != nil {
		return err
	}

	reader := io.NewSectionReader(in, offset, length)
	writer := io.NewOffsetWriter(out, offset)
	buf := make([]byte, bufferSize)
	for {
		if err := t.ctx.Err(); err != nil {
			out.Close()
			return err
		}
		n, readErr := reader.Read(buf)
		if n > 0 {
			if _, err := writer.Write(buf[:n]); err != nil {
				out.Close()
				return err
			}
			t.transferred.Add(int64(n))
			m.bytesTransferred.Add(int64(n))
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			out.Close()
			return readErr
		}
	}
	return out.Close()
}

// sides returns (source, destination) for a direction.
func (m *Manager) sides(dir Direction, remote vfs.FileSystem) (vfs.FileSystem, vfs.FileSystem) {
	if dir == Upload {
		return m.local, remote
	}
	return remote, m.local
}

func (m *Manager) find(id int) *Transfer {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.transfers {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (m *Manager) remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transfers = slices.DeleteFunc(m.transfers, func(t *Transfer) bool { return t.ID == id })
}

func relativeTo(fsys vfs.FileSystem, base, path string) string {
	if base == path || base == "" {
		return fsys.Base(path)
	}
	rel := path[len(base):]
	for len(rel) > 0 && (rel[0] == '/' || rel[0] == '\\') {
		rel = rel[1:]
	}
	return rel
}
