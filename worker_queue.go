package ethin

//Worker队列的接口和工厂模式，是antsm库中worker管理的核心抽象层

import (
	"errors"
	"time"
)

var errQueueIsFull = errors.New("the workerQueue is full")

type worker interface {
	run()
	finish()
	lastUsedTime() time.Time
	setLastUsedTime(t time.Time)
	inputFunc(func())
	inputArg(any)
}

type workerQueue interface {
	len() int
	isEmpty() bool
	insert(worker worker) error
	detach() worker
	refresh(duration time.Duration) []worker
	reset()
}

type queueType int

const (
	queueTypeStack queueType = 1 << iota
	queueTypeLoop
)

// TODO 工厂函数
func newWorkerQueue(t queueType, size int) workerQueue {
	switch t {
	case queueTypeStack:
		return nil
	case queueTypeLoop:
		return nil
	default:
		return nil
	}
}
