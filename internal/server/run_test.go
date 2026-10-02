package server

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	s := New(nil, nil, &fakePinger{}, Options{Tokens: fullAccess{}, Port: "0"})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v after cancel, want nil", err)
		}
	case <-time.After(shutdownTimeout + time.Second):
		t.Fatal("Run didn't return after the context was cancelled")
	}
}

func TestRunReportsListenErrors(t *testing.T) {
	taken, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	_, port, _ := net.SplitHostPort(taken.Addr().String())

	s := New(nil, nil, &fakePinger{}, Options{Tokens: fullAccess{}, Port: port})
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("Run succeeded on a port that's already in use")
	}
}
