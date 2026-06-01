package lint

import "context"

type ctxKey struct{}

// ContextWithRunner returns a context that carries the lint Runner.
func ContextWithRunner(ctx context.Context, r *Runner) context.Context {
	return context.WithValue(ctx, ctxKey{}, r)
}

// FromContext returns the lint Runner from the context, or nil.
func FromContext(ctx context.Context) *Runner {
	r, _ := ctx.Value(ctxKey{}).(*Runner)
	return r
}
