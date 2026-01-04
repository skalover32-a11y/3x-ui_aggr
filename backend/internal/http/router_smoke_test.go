package httpapi

import "testing"

func TestRouterInit(t *testing.T) {
	h := &Handler{JWTSecret: []byte("test")}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("router init panic: %v", r)
		}
	}()
	_ = NewRouter(h)
}
