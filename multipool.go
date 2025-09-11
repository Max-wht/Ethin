package ethin

import "math"

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
