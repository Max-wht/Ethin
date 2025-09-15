package ethin

import (
	"context"
	"errors"
	"log"
	"math"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

//ethin.go 实现了poolCommon的基本逻辑（调度，清理，时间缓存，生命周期）

const (
	DefaultAntsmPoolSize = math.MaxInt32 //默认协程池大小

	DefaultCleanIntervalTime = time.Second //过期清理的默认周期
)

const (
	OPENED = iota

	CLOSED
)

var (
	ErrLackPoolFunc = errors.New("must provide a function for the pool")

	ErrInvalidPoolExpiry = errors.New("must provide an invalid expiry time")

	ErrInvalidPoolIndex = errors.New("invalid pool size")

	ErrInvalidPreAllocSize = errors.New("can not set up a negative capacity in preAlloc mode")

	ErrPoolClosed = errors.New("this pool has been closed")

	ErrPoolOverload = errors.New("too many goroutines blocked on submit or Nonblocking is set")

	ErrInvalidMultiPoolSize = errors.New("invalid size for multiple pool")

	ErrInvalidLoadBalancingStrategy = errors.New("invalid load-balancing strategy")

	ErrTimeout = errors.New("operation time out")

	// 0 --> 单个调度器-->无缓冲通道
	// 1 --> 多个调度器-->缓冲通道
	workerChanCap = func() int {
		if runtime.GOMAXPROCS(0) == 1 {
			return 0
		}
		return 1
	}()

	defaultLogger = Logger(log.New(os.Stderr, "[antsm ==>]", log.LstdFlags|log.Lmsgprefix|log.Lmicroseconds))

	defaultAntsmPool, _ = NewPool(DefaultAntsmPoolSize)
)

type poolCommon struct {
	capacity    int32         //协程池的最大容量， <0 表示无限容量
	running     int32         //当前运行的worker数量（原子计数）
	lock        sync.Locker   //自旋锁
	workers     workerQueue   //空闲的worker队列（stack or loop）
	state       int32         //运行or关闭状态
	cond        *sync.Cond    //没有空闲时阻塞等待
	allDone     chan struct{} //所有worker退出的通知信号
	once        *sync.Once    //确保pool只被关闭一次
	workerCache sync.Pool     //新worker缓存
	waiting     int32         //当前在submit上等待可用worker的阻塞协程数

	purgeDone int32              //过期清理协程是否已经结束
	purgeCtx  context.Context    //后台清理任务上下文，用于退出控制
	stopPurge context.CancelFunc //停止过期协程清理

	tick2Done   int32              //更新时间缓存是否已经结束
	tick2Ctx    context.Context    //时钟协程的上下文，用于退出控制
	stopTick2Ck context.CancelFunc //停止时钟协程

	now     atomic.Value //缓存当前时间
	options *Options     //配置信息
}

type Logger interface {
	//使用log.Printf()
	Printf(format string, args ...any)
}

func newPool(size int, options ...Option) (*poolCommon, error) {
	if size <= 0 {
		size = -1
	}

	opt := loadOptions(options...)

	if !opt.DisablePurge {
		if expiry := opt.ExpiryDuration; expiry < 0 {
			return nil, ErrInvalidPoolExpiry
		} else if expiry == 0 {
			opt.ExpiryDuration = DefaultCleanIntervalTime
		}
	}

	if opt.Logger == nil {
		opt.Logger = defaultLogger
	}

	p := &poolCommon{
		capacity: int32(size),
		allDone:  make(chan struct{}),
		lock:     &sync.Mutex{},
		once:     &sync.Once{},
		options:  opt,
	}
	if p.options.PreAlloc {
		if size == -1 {
			return nil, ErrInvalidPreAllocSize
		}
		p.workers = newWorkerQueue(queueTypeLoop, size)
	} else {
		p.workers = newWorkerQueue(queueTypeStack, size)
	}

	p.cond = sync.NewCond(p.lock)

	p.goPurge()
	p.goTick2Ck()

	return p, nil
}

// 过期worker清理
func (p *poolCommon) goPurge() {
	if p.options.DisablePurge {
		return
	}
	p.purgeCtx, p.stopPurge = context.WithCancel(context.Background())

	go p.purgeStaleWorker()
}

func (p *poolCommon) goTick2Ck() {
	p.now.Store(time.Now())
	p.tick2Ctx, p.stopTick2Ck = context.WithCancel(context.Background())
	go p.tick2Ck()
}

func (p *poolCommon) purgeStaleWorker() {
	ticker := time.NewTicker(p.options.ExpiryDuration)

	defer func() {
		ticker.Stop()
		atomic.StoreInt32(&p.purgeDone, 1)
	}()

	purgeCtx := p.purgeCtx
	for {
		select {
		case <-purgeCtx.Done():
			return
		case <-ticker.C:
		}

		if p.IsClose() {
			break
		}

		var isDormant bool
		p.lock.Lock()
		staleWorkers := p.workers.refresh(p.options.ExpiryDuration)
		n := p.Running()
		isDormant = n == 0 || n == len(staleWorkers)
		p.lock.Unlock()

		for i := range staleWorkers {
			staleWorkers[i].finish()
			staleWorkers[i] = nil
		}

		if isDormant && p.Waiting() > 0 {
			p.cond.Broadcast()
		}
	}
}

const nowTimeUpdateIn = 500 * time.Millisecond

func (p *poolCommon) tick2Ck() {
	ticker := time.NewTicker(nowTimeUpdateIn)
	defer func() {
		ticker.Stop()
		atomic.StoreInt32(&p.tick2Done, 1)
	}()

	tick2CkCtx := p.tick2Ctx
	for {
		select {
		case <-tick2CkCtx.Done():
			return
		case <-ticker.C:
		}

		if p.IsClose() {
			break
		}

		p.now.Store(time.Now())
	}
}

// 复用worker
func (p *poolCommon) retrieveWorker() (w worker, err error) {
	p.lock.Lock()

retry:
	if w = p.workers.detach(); w != nil {
		p.lock.Unlock()
		return
	}

	if capacity := p.Cap(); capacity == -1 || capacity > p.Running() {
		p.lock.Unlock()
		w = p.workerCache.Get().(worker)
		w.run()
		return
	}

	if p.options.NonBlocking || (p.options.MaxBlockingTasks != 0 && p.Waiting() >= p.options.MaxBlockingTasks) {
		p.lock.Unlock()
		return nil, ErrPoolOverload
	}

	p.addWaiting(1)
	p.cond.Wait()
	p.addWaiting(-1)

	if p.IsClose() {
		p.lock.Unlock()
		return nil, ErrPoolClosed
	}

	goto retry
}

// 检测Worker是否需要回收
func (p *poolCommon) revertWorker(worker worker) bool {
	if capacity := p.Cap(); (capacity > 0 && p.Running() > capacity) || p.IsClose() {
		p.cond.Broadcast()
		return false
	}

	worker.setLastUsedTime(p.nowTime())

	p.lock.Lock()
	if p.IsClose() {
		p.lock.Unlock()
		return false
	}
	if err := p.workers.insert(worker); err != nil {
		p.lock.Unlock()
		return false
	}
	p.cond.Signal()
	return true
}

func (p *poolCommon) IsClose() bool {
	return atomic.LoadInt32(&p.state) == CLOSED
}

func (p *poolCommon) Running() int {
	return int(atomic.LoadInt32(&p.running))
}

func (p *poolCommon) Waiting() int {
	return int(atomic.LoadInt32(&p.waiting))
}

func (p *poolCommon) addRunning(delta int) int {
	return int(atomic.AddInt32(&p.running, int32(delta)))
}

func (p *poolCommon) Cap() int {
	return int(atomic.LoadInt32(&p.capacity))
}

func (p *poolCommon) Free() int {
	c := p.Cap()
	if c < 0 {
		return -1
	}
	return c - p.Running()
}

func (p *poolCommon) nowTime() time.Time {
	return p.now.Load().(time.Time)
}

func (p *poolCommon) addWaiting(delta int) {
	atomic.AddInt32(&p.waiting, int32(delta))
}

func (p *poolCommon) Tune(size int) {
	capacity := p.Cap()
	if capacity == -1 || size < 0 || capacity == size {
		return
	}
	atomic.StoreInt32(&p.capacity, int32(size))
	if size > capacity {
		if size-capacity == 1 {
			p.cond.Signal()
			return
		}
		p.cond.Broadcast()
	}
}

func (p *poolCommon) Release() {
	if !atomic.CompareAndSwapInt32(&p.state, OPENED, CLOSED) {
		return
	}

	if p.stopPurge != nil {
		p.stopPurge()
		p.stopPurge = nil
	}

	if p.stopTick2Ck != nil {
		p.stopTick2Ck()
		p.stopTick2Ck = nil
	}

	p.lock.Lock()
	p.workers.reset()
	p.lock.Unlock()

	p.cond.Broadcast()
}

func (p *poolCommon) ReleaseTimeout(timeout time.Duration) error {
	if p.IsClose() || (p.options.DisablePurge && p.stopPurge == nil) || p.stopTick2Ck == nil {
		return ErrPoolClosed
	}

	p.Release()

	var purgeCh <-chan struct{}
	if !p.options.DisablePurge {
		purgeCh = p.purgeCtx.Done()
	} else {
		purgeCh = p.allDone
	}

	if p.Running() == 0 {
		p.once.Do(func() {
			close(p.allDone)
		})
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return ErrTimeout
		case <-p.allDone:
			<-purgeCh
			<-p.tick2Ctx.Done()
			if p.Running() == 0 &&
				(p.options.DisablePurge || atomic.LoadInt32(&p.purgeDone) == 1) &&
				atomic.LoadInt32(&p.tick2Done) == 1 {
				return nil
			}
		}

	}
}
