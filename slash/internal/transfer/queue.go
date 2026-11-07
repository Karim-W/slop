package transfer

import "sync"

type chunk struct {
	transfer *Transfer
	offset   int64
	length   int64
}

// chunkQueue is an unbounded FIFO that blocks consumers until work arrives.
type chunkQueue struct {
	mu     sync.Mutex
	ready  *sync.Cond
	items  []chunk
	closed bool
}

func newChunkQueue() *chunkQueue {
	q := &chunkQueue{}
	q.ready = sync.NewCond(&q.mu)
	return q
}

func (q *chunkQueue) push(chunks ...chunk) {
	q.mu.Lock()
	q.items = append(q.items, chunks...)
	q.mu.Unlock()
	q.ready.Broadcast()
}

func (q *chunkQueue) pop() (chunk, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.ready.Wait()
	}
	if q.closed {
		return chunk{}, false
	}
	next := q.items[0]
	q.items = q.items[1:]
	return next, true
}

func (q *chunkQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.ready.Broadcast()
}
