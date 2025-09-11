package ethin

import "time"

// Option represent ths optional funciton
type Option func(opt *Options)

// loadOptions
func loadOptions(options ...Option) *Options {
	opts := new(Options)
	for _, option := range options {
		option(opts)
	}
	return opts
}

type Options struct {
	//清理协程的扫描周期
	ExpiryDuration time.Duration

	//是否初始化的时候为worker队列预分配内存
	PreAlloc bool

	//最大阻塞容量
	MaxBlockingTasks int

	//非阻塞提交模式
	NonBlocking bool

	//Panic处理器，未设置则记录日志和堆栈
	PanicHandler func(any)

	//Logger接口
	Logger Logger

	//是否关闭过期worker的自动清理。
	//为true的时候，worker不会按照ExpiryDuration被回收
	DisablePurge bool
}

// Functional Options Pattern
func WithOptions(options Options) Option {
	return func(opts *Options) {
		*opts = options
	}
}

func WithExpiryDuration(duration time.Duration) Option {
	return func(opts *Options) {
		opts.ExpiryDuration = duration
	}
}

func WithMaxBlockingTasks(i int) Option {
	return func(opts *Options) {
		opts.MaxBlockingTasks = i
	}
}

func WithPreAlloc(b bool) Option {
	return func(opts *Options) {
		opts.PreAlloc = b
	}
}

func WithNonBlocking(b bool) Option {
	return func(opts *Options) {
		opts.NonBlocking = b
	}
}

func WithPanicHandler(f func(any)) Option {
	return func(opts *Options) {
		opts.PanicHandler = f
	}
}

func WithLogger(log Logger) Option {
	return func(opts *Options) {
		opts.Logger = log
	}
}

func WithDisablePurge(b bool) Option {
	return func(opts *Options) {
		opts.DisablePurge = b
	}
}
