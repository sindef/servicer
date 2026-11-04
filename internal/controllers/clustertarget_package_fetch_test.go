package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// withPackageFetchBounds shrinks the fetch bounds for one test and restores the
// production defaults afterwards. The bounds are package variables, so these
// tests must not run in parallel.
func withPackageFetchBounds(t *testing.T, timeout time.Duration, limit int64) {
	t.Helper()
	previousTimeout, previousLimit := packageFetchTimeout, packageFetchLimit
	packageFetchTimeout, packageFetchLimit = timeout, limit
	t.Cleanup(func() {
		packageFetchTimeout, packageFetchLimit = previousTimeout, previousLimit
	})
}

// blockUntilRequestDone keeps a request open until the client gives up, which is
// what a hung operator manifest host looks like to the reconciler.
func blockUntilRequestDone(w http.ResponseWriter, r *http.Request) {
	<-r.Context().Done()
}

func TestFetchURLBytesTimesOutOnHungServer(t *testing.T) {
	withPackageFetchBounds(t, 200*time.Millisecond, defaultPackageFetchLimit)

	server := httptest.NewServer(http.HandlerFunc(blockUntilRequestDone))
	defer server.Close()

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := fetchURLBytes(context.Background(), server.URL)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from a hung server, got nil")
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("fetchURLBytes returned after %s, want the configured 200ms timeout", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fetchURLBytes blocked on a hung server instead of honouring its timeout")
	}
}

func TestFetchURLBytesHonoursContextCancellation(t *testing.T) {
	withPackageFetchBounds(t, time.Minute, defaultPackageFetchLimit)

	server := httptest.NewServer(http.HandlerFunc(blockUntilRequestDone))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := fetchURLBytes(ctx, server.URL)
		done <- err
	}()
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after context cancellation, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fetchURLBytes ignored reconcile context cancellation")
	}
}

func TestFetchURLBytesRejectsOversizedBody(t *testing.T) {
	withPackageFetchBounds(t, 5*time.Second, 1024)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Flush so the response is chunked and carries no Content-Length: the
		// size cap must come from reading the body, not from the header.
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		if _, err := w.Write([]byte(strings.Repeat("a", 8192))); err != nil {
			return
		}
	}))
	defer server.Close()

	body, err := fetchURLBytes(context.Background(), server.URL)
	if err == nil {
		t.Fatalf("expected an error for an oversized body, got %d bytes", len(body))
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("error %q does not mention the size limit", err)
	}
}

func TestFetchURLBytesRejectsOversizedContentLength(t *testing.T) {
	withPackageFetchBounds(t, 5*time.Second, 1024)

	payload := strings.Repeat("a", 8192)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "8192")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(payload)); err != nil {
			return
		}
	}))
	defer server.Close()

	if _, err := fetchURLBytes(context.Background(), server.URL); err == nil {
		t.Fatal("expected an error for an oversized Content-Length, got nil")
	} else if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("error %q does not mention the size limit", err)
	}
}

func TestFetchURLBytesReturnsBodyWithinBounds(t *testing.T) {
	withPackageFetchBounds(t, 5*time.Second, 1024)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("apiVersion: v1\nkind: Namespace\n")); err != nil {
			return
		}
	}))
	defer server.Close()

	body, err := fetchURLBytes(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("fetchURLBytes returned an error for an in-bounds body: %v", err)
	}
	if got, want := string(body), "apiVersion: v1\nkind: Namespace\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestFetchURLBytesRejectsNonOKStatus(t *testing.T) {
	withPackageFetchBounds(t, 5*time.Second, 1024)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	if _, err := fetchURLBytes(context.Background(), server.URL); err == nil {
		t.Fatal("expected an error for HTTP 404, got nil")
	} else if !strings.Contains(err.Error(), "404") {
		t.Fatalf("error %q does not report the HTTP status", err)
	}
}
