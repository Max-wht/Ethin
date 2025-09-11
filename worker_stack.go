package ethin

import "time"

//WorkerStack
//1.--> 插入worker
//2.--> 取出worker
//3.--> 过期清理
//4.--> 二分查找优化

type workerStack struct {
	items  []worker //空闲的worker
	expire []worker //过期的worker
}

// 返回workerStack实例
func newWorkerStack(size int) *workerStack {
	return &workerStack{
		items: make([]worker, 0, size), //预分配内存
	}
}

func (ws *workerStack) len() int {
	return len(ws.items)
}

func (ws *workerStack) isEmpty() bool {
	return len(ws.items) == 0
}

func (ws *workerStack) insert(w worker) error {
	ws.items = append(ws.items, w)
	return nil
}

func (ws *workerStack) detach() worker {
	l := ws.len()
	if l == 0 {
		return nil
	}

	w := ws.items[l-1]
	ws.items[l-1] = nil
	ws.items = ws.items[0 : l-1]

	return w
}

// 查询最后一个worker过期的位置
func (ws *workerStack) binarySearch(l, r int, expiryTime time.Time) int {
	for l <= r {
		mid := l + ((r - l) >> 1)
		if expiryTime.Before(ws.items[mid].lastUsedTime()) {
			r = mid - 1
		} else {
			l = mid + 1
		}
	}

	return r
}

func (ws *workerStack) refresh(duration time.Duration) []worker {
	n := ws.len()
	if n == 0 {
		return nil
	}

	expireTime := time.Now().Add(-duration)
	index := ws.binarySearch(0, n-1, expireTime)

	ws.expire = ws.expire[:0]
	if index != -1 {
		ws.expire = append(ws.expire, ws.items[:index+1]...)
		m := copy(ws.items, ws.items[index+1:])
		for i := m; i < n; i++ {
			ws.items[i] = nil
		}
		ws.items = ws.items[:m]
	}

	return ws.expire
}

func (ws *workerStack) reset() {
	for i := 0; i < ws.len(); i++ {
		ws.items[i].finish()
		ws.items[i] = nil
	}
	ws.items = ws.items[:0]
}
