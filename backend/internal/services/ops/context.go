package ops

import "context"

type rebootConfirmKey struct{}

func withRebootConfirm(ctx context.Context, confirm string) context.Context {
	if ctx == nil {
		return context.WithValue(context.Background(), rebootConfirmKey{}, confirm)
	}
	return context.WithValue(ctx, rebootConfirmKey{}, confirm)
}

func rebootConfirmFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	val := ctx.Value(rebootConfirmKey{})
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}
