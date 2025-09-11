package ethin

type Pool struct {
	*poolCommon
}

func (p *Pool) Submit(task func()) error {
	if p.IsClose() {
		return ErrPoolClosed
	}

	w, err := p.retrieveWorker()
	if w != nil {
		w.inputFunc(task)
	}
	return err
}

func NewPool(size int, option ...Option) (*Pool, error) {
	pc, err := newPool(size, option...)
	if err != nil {
		return nil, err
	}

	pool := &Pool{
		poolCommon: pc,
	}

	pool.workerCache.New = func() any {
		return &goWorker{
			pool: pool,
			task: make(chan func(), workerChanCap),
		}
	}

	return pool, nil
}
