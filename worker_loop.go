package ethin

import "time"

type loopQueue struct {
	items  []worker
	expiry []worker
	head   int
	tail   int
	size   int
	isFull bool
}

func newLoopQueue(size int) *loopQueue {
	if size <= 0 {
		return nil
	}

	return &loopQueue{
		items: make([]worker, size),
		size:  size,
	}
}

func (wl *loopQueue) isEmpty() bool {
	return wl.head == wl.tail && !wl.isFull
}

func (wl *loopQueue) len() int {
	if wl.size == 0 || wl.isEmpty() {
		return 0
	}

	if wl.head == wl.tail && wl.isFull {
		return wl.size
	}

	if wl.tail > wl.head {
		return wl.tail - wl.head
	}

	return wl.size - wl.head + wl.tail
}

func (wl *loopQueue) insert(w worker) error {
	if wl.isFull {
		return errQueueIsFull
	}

	wl.items[wl.tail] = w
	wl.tail = (wl.tail + 1) % wl.size

	if wl.head == wl.tail {
		wl.isFull = true
	}

	return nil
}

func (wl *loopQueue) detach() worker {
	if wl.isEmpty() {
		return nil
	}

	w := wl.items[wl.head]
	wl.items[wl.head] = nil
	wl.head = (wl.head + 1) % wl.size

	return w
}

func (wl *loopQueue) binarySearch(expiryTime time.Time) int {
	var mid, nlen, basel, tmid int
	nlen = len(wl.items)

	if wl.isEmpty() || expiryTime.Before(wl.items[wl.head].lastUsedTime()) {
		return -1
	}

	l := 0
	basel = wl.head
	r := (wl.tail - 1 - wl.head + nlen) % nlen

	for l <= r {
		mid = l + ((r - l) >> 1)
		tmid = (basel + mid + nlen) % nlen
		if expiryTime.Before(wl.items[tmid].lastUsedTime()) {
			r = mid - 1
		} else {
			l = mid + 1
		}
	}

	return (r + basel + nlen) % nlen
}

func (wl *loopQueue) refresh(duration time.Duration) []worker {
	expiryTime := time.Now().Add(-duration)
	index := wl.binarySearch(expiryTime)
	if index == -1 {
		return nil
	}
	wl.expiry = wl.expiry[:0]

	if wl.head <= index {
		wl.expiry = append(wl.expiry, wl.items[wl.head:index+1]...)
		for i := wl.head; i < index+1; i++ {
			wl.items[i] = nil
		}
	} else {
		wl.expiry = append(wl.expiry, wl.items[0:index+1]...)
		wl.expiry = append(wl.expiry, wl.items[wl.head:]...)
		for i := 0; i < index+1; i++ {
			wl.items[i] = nil
		}
		for i := wl.head; i < wl.size; i++ {
			wl.items[i] = nil
		}
	}

	head := (index + 1) % wl.size
	wl.head = head
	if len(wl.expiry) > 0 {
		wl.isFull = false
	}

	return wl.expiry
}

func (wl *loopQueue) reset() {
	if wl.isEmpty() {
		return
	}

retry:
	if w := wl.detach(); w != nil {
		w.finish()
		goto retry
	}

	wl.head = 0
	wl.tail = 0
}
