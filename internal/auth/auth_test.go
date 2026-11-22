package auth

import (
	"testing"
	"time"
)

func TestSignAndAllow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	token, err := Sign("dev-secret", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed("Bearer "+token, "dev-secret", now.Add(time.Minute)) {
		t.Fatal("fresh token rejected")
	}
	if allowed("Bearer "+token, "other", now) {
		t.Fatal("wrong secret accepted")
	}
	if allowed(token, "dev-secret", now) {
		t.Fatal("missing bearer accepted")
	}
	expired, err := Sign("dev-secret", time.Second, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if allowed("Bearer "+expired, "dev-secret", now) {
		t.Fatal("expired token accepted")
	}
	if _, err := Sign("", time.Hour, now); err != ErrSecret {
		t.Fatalf("empty secret err=%v", err)
	}
}
