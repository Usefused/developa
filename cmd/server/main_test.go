package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServeReturnsSanitizedBindError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Addr: listener.Addr().String(), ReadHeaderTimeout: time.Second}
	err = serve(context.Background(), server, time.Second)
	if err == nil || strings.Contains(err.Error(), listener.Addr().String()) {
		t.Fatal("expected a sanitized bind error")
	}
}

func TestServeAcceptsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	server := &http.Server{Addr: "127.0.0.1:0", ReadHeaderTimeout: time.Second}
	if err := serve(ctx, server, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownDrainsActiveRequest(t *testing.T) {
	started, release, completed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
		close(completed)
	}))
	defer server.Close()
	response := make(chan error, 1)
	go func() {
		res, err := http.Get(server.URL)
		if err == nil {
			_ = res.Body.Close()
		}
		response <- err
	}()
	<-started
	shutdown := make(chan error, 1)
	go func() { shutdown <- shutdownServer(server.Config, time.Second) }()
	close(release)
	if err := <-shutdown; err != nil {
		t.Fatal(err)
	}
	<-completed
	if err := <-response; err != nil {
		t.Fatal(err)
	}
}

func TestServerClosedIsNormal(t *testing.T) {
	if err := serverError(http.ErrServerClosed); err != nil {
		t.Fatal(err)
	}
	if err := serverError(errors.New("secret")); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("unexpected server failure was not sanitized")
	}
}
