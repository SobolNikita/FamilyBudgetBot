package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestHTTPShutdownDrainsRequestsAndCloseCancelsOverdueOnes(t *testing.T) {
	for _, force := range []bool{false, true} {
		name := "drain"
		if force {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			entered, release, canceled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			server := newHTTPServer("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
					_, _ = io.WriteString(w, "completed")
				case <-r.Context().Done():
					close(canceled)
				}
			}))
			if server.BaseContext != nil {
				t.Fatal("request context must not inherit the process signal")
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = server.Close() })
			go func() { _ = server.Serve(listener) }()
			result := make(chan string, 1)
			go func() {
				client := &http.Client{Timeout: 5 * time.Second}
				response, err := client.Get("http://" + listener.Addr().String())
				if err != nil {
					result <- "request failed"
					return
				}
				defer response.Body.Close()
				body, _ := io.ReadAll(response.Body)
				result <- string(body)
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not start")
			}
			stopping := make(chan struct{})
			server.RegisterOnShutdown(func() { close(stopping) })
			shutdown := make(chan error, 1)
			budget := 5 * time.Second
			if force {
				budget = 30 * time.Millisecond
			}
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), budget)
				defer cancel()
				shutdown <- server.Shutdown(ctx)
			}()
			<-stopping
			if force {
				if err := <-shutdown; !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected deadline: %v", err)
				}
				_ = server.Close()
				select {
				case <-canceled:
				case <-time.After(5 * time.Second):
					t.Fatal("forced shutdown left request running")
				}
			} else {
				close(release)
				if got := <-result; got != "completed" {
					t.Fatalf("in-flight request interrupted: %s", got)
				}
				if err := <-shutdown; err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
