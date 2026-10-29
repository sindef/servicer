package controllers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// useFetchClientTimeout swaps the package-level fetch client for the duration of
// a test so timeout behaviour can be exercised without waiting a full minute.
func useFetchClientTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	original := packageFetchClient
	packageFetchClient = &http.Client{Timeout: timeout}
	t.Cleanup(func() { packageFetchClient = original })
}

func TestFetchURLBytesTimesOutOnBlockedServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(server.Close)
	useFetchClientTimeout(t, 200*time.Millisecond)

	start := time.Now()
	body, err := fetchURLBytes(context.Background(), server.URL)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected timeout error, got body of %d bytes", len(body))
	}
	if elapsed > 5*time.Second {
		t.Fatalf("fetch took %s, expected to fail near the 200ms client timeout", elapsed)
	}
}

func TestFetchURLBytesReturnsErrorOnOversizedBody(t *testing.T) {
	const chunkSize = 256 << 10
	chunk := bytes.Repeat([]byte("a"), chunkSize)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		for sent := int64(0); sent <= maxPackageFetchBytes; sent += chunkSize {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	body, err := fetchURLBytes(context.Background(), server.URL)
	if err == nil {
		t.Fatalf("expected size limit error, got body of %d bytes", len(body))
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected error about body size limit, got %v", err)
	}
	if int64(len(body)) > maxPackageFetchBytes+1 {
		t.Fatalf("read %d bytes, expected at most %d", len(body), maxPackageFetchBytes+1)
	}
}

func TestFetchURLBytesReadsBodyWithinLimit(t *testing.T) {
	const payload = "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: operators\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	body, err := fetchURLBytes(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("fetchURLBytes returned error: %v", err)
	}
	if string(body) != payload {
		t.Fatalf("unexpected body %q", string(body))
	}
}

func TestFetchURLBytesRejectsNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	if _, err := fetchURLBytes(context.Background(), server.URL); err == nil {
		t.Fatal("expected error for HTTP 404 response")
	}
}

func TestFetchURLBytesHonoursContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(server.Close)

	// Long client timeout: cancellation must come from the reconcile context.
	useFetchClientTimeout(t, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := fetchURLBytes(ctx, server.URL); err == nil {
		t.Fatal("expected error after context deadline")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("fetch took %s, expected cancellation near the 100ms context deadline", elapsed)
	}
}
