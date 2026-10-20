package trace

import "context"

type ctxKey struct{}

// WithID 把 traceId 放入 context。
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext 读取 traceId，缺失时返回空串。
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}
