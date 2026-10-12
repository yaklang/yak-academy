// Package eventbus 提供进程内 append-only 事件总线。
// 职责：事件落库（INSERT-only）+ 内存广播给派生器。
// Phase 2 可替换为 Redis Stream/MQ，但 Append/Publish 接口不变。
package eventbus

import (
	"context"
	"sync"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// Handler 事件消费者（派生器实现）。
type Handler func(ctx context.Context, ev model.Event)

type Bus struct {
	mu       sync.Mutex
	handlers []Handler
}

func New() *Bus {
	return &Bus{}
}

// Subscribe 注册事件消费者。派生器在启动时调用。
func (b *Bus) Subscribe(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, h)
}

// Publish 事件落库由 repo 层保证（INSERT-only），这里仅做内存广播。
// 注意：handler 执行不阻塞发布方（异步语义由调用方/派生器决定）。
func (b *Bus) Publish(ctx context.Context, ev model.Event) {
	b.mu.Lock()
	handlers := make([]Handler, len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.Unlock()
	for _, h := range handlers {
		h(ctx, ev)
	}
}