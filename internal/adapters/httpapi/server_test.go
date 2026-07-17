package httpapi

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestRunServerGracefulShutdown(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	s := NewServer(ServerConfig{Address: addr, ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	done := make(chan error, 1)
	go func() { done <- RunServer(ctx, s) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown timed out")
	}
}
