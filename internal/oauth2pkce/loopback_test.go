package oauth2pkce

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestLoopbackServerCapturesCodeAndState(t *testing.T) {
	ls, err := StartLoopback()
	if err != nil {
		t.Fatalf("StartLoopback: %v", err)
	}
	defer ls.Close()

	go func() {
		time.Sleep(20 * time.Millisecond)
		http.Get(ls.RedirectURI() + "?code=abc123&state=xyz789")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res, err := ls.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.Code != "abc123" || res.State != "xyz789" {
		t.Errorf("result = %+v", res)
	}
}

func TestLoopbackServerCapturesDenial(t *testing.T) {
	ls, err := StartLoopback()
	if err != nil {
		t.Fatalf("StartLoopback: %v", err)
	}
	defer ls.Close()

	go func() {
		time.Sleep(20 * time.Millisecond)
		http.Get(ls.RedirectURI() + "?error=access_denied")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res, err := ls.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.Err == nil {
		t.Fatal("expected a denial to surface as an error on the result")
	}
}

func TestLoopbackServerTimesOut(t *testing.T) {
	ls, err := StartLoopback()
	if err != nil {
		t.Fatalf("StartLoopback: %v", err)
	}
	defer ls.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := ls.Wait(ctx); err == nil {
		t.Fatal("expected Wait to time out when nothing ever redirects")
	}
}
