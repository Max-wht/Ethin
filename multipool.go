package ethin

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

//主要提供多池调度器，负载均衡

type LoadBalacningStrategy int

const (
	// RoundRobin 原子递增索引轮训方法
	RoundRobin LoadBalacningStrategy = 1 << (iota + 1) //2

	// LeastTasks 线性选择Running()最少的池
	LeastTasks //4
)

type MultiPool struct {
	pools []*Pool
	index uint32
	state int32
	lbs   LoadBalacningStrategy
}

func NewMultiPool(size, sizePerPool int, lbs LoadBalacningStrategy, options ...Option) (m *MultiPool, err error) {
	if size <= 0 {
		return nil, ErrInvalidMultiPoolSize
	}

	if lbs != RoundRobin && lbs != LeastTasks {
		return nil, ErrInvalidLoadBalancingStrategy
	}

	p := make([]*Pool, size)
	for i := 0; i < size; i++ {
		pool, err := NewPool(sizePerPool, options...)
		if err != nil {
			return nil, err
		}
		p[i] = pool
	}
	return &MultiPool{
		pools: p,
		index: math.MaxUint32,
		lbs:   lbs,
	}, nil
}

func (mp *MultiPool) next(lbs LoadBalacningStrategy) (idx int) {
	switch lbs {
	case RoundRobin:
		return int(atomic.AddUint32(&mp.index, 1) % uint32(len(mp.pools)))
	case LeastTasks:
		miniFunc := 1<<31 - 1
		for i, pool := range mp.pools {
			if pool.Running() < miniFunc {
				miniFunc = pool.Running()
				idx = i
			}
		}
		return
	}
	return -1
}

func (mp *MultiPool) Submit(f func()) (err error) {
	if mp.IsColsed() {
		return ErrPoolClosed
	}
	index := mp.next(mp.lbs)
	pool := mp.pools[index]
	if err = pool.Submit(f); err == nil {
		return
	}
	if err == ErrPoolOverload && mp.lbs == RoundRobin {
		return mp.pools[mp.next(LeastTasks)].Submit(f)
	}
	return
}

func (mp *MultiPool) Running() (n int) {
	for _, pool := range mp.pools {
		n += pool.Running()
	}
	return
}

func (mp *MultiPool) RunningByIndex(i int) (n int, err error) {
	if i < 0 || i >= len(mp.pools) {
		return -1, ErrInvalidPoolIndex
	}
	return mp.pools[i].Running(), nil
}

func (mp *MultiPool) Free() (n int) {
	for _, pool := range mp.pools {
		n += pool.Free()
	}
	return
}

func (mp *MultiPool) FreeByIndex(idx int) (int, error) {
	if idx < 0 || idx >= len(mp.pools) {
		return -1, ErrInvalidPoolIndex
	}
	return mp.pools[idx].Free(), nil
}

func (mp *MultiPool) Waiting() (n int) {
	for _, pool := range mp.pools {
		n += pool.Waiting()
	}
	return
}

func (mp *MultiPool) WaitingByIndex(idx int) (int, error) {
	if idx < 0 || idx >= len(mp.pools) {
		return -1, ErrInvalidPoolIndex
	}
	return mp.pools[idx].Waiting(), nil
}

func (mp *MultiPool) Cap() (n int) {
	for _, pool := range mp.pools {
		n += pool.Cap()
	}
	return
}

func (mp *MultiPool) IsColsed() bool {
	return atomic.LoadInt32(&mp.state) == CLOSED // ==1
}

func (mp *MultiPool) ReleaseTimeout(timeout time.Duration) error {
	if !atomic.CompareAndSwapInt32(&mp.state, OPENED, CLOSED) {
		return ErrPoolClosed
	}

	errCh := make(chan error, len(mp.pools))

	var wg errgroup.Group
	for i, pool := range mp.pools {
		wg.Go(func() error {
			err := pool.ReleaseTimeout(timeout)
			if err != nil {
				err = fmt.Errorf("pool %d: %v", i, err)
			}
			errCh <- err
			return err
		})
	}

	_ = wg.Wait()

	var errStr strings.Builder
	for i := 0; i < len(mp.pools); i++ {
		if err := <-errCh; err != nil {
			errStr.WriteString(err.Error())
			errStr.WriteString(" | ")
		}
	}

	if errStr.Len() == 0 {
		return nil
	}

	return errors.New(strings.TrimSuffix(errStr.String(), " | "))
}

func (mp *MultiPool) Reboot() {
	if atomic.CompareAndSwapInt32(&mp.state, CLOSED, OPENED) {
		atomic.StoreUint32(&mp.index, 0)
		for _, pool := range mp.pools {
			pool.Reboot()
		}
	}
}
