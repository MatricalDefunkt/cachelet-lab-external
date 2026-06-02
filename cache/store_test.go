package cache

import (
	"testing"
	"time"
)

func TestSetGet(t *testing.T) {
	s := New(0)
	defer s.Close()

	s.Set("a", "1", 0)
	if v, ok := s.Get("a"); !ok || v != "1" {
		t.Fatalf("Get(a) = %q, %v; want \"1\", true", v, ok)
	}
}

func TestGetMissing(t *testing.T) {
	s := New(0)
	defer s.Close()

	if _, ok := s.Get("nope"); ok {
		t.Fatal("Get(nope) = ok; want missing")
	}
}

func TestSetReplaces(t *testing.T) {
	s := New(0)
	defer s.Close()

	s.Set("a", "1", 0)
	s.Set("a", "2", 0)
	if v, _ := s.Get("a"); v != "2" {
		t.Fatalf("Get(a) = %q; want \"2\"", v)
	}
	if n := s.Len(); n != 1 {
		t.Fatalf("Len = %d; want 1", n)
	}
}

func TestDelete(t *testing.T) {
	s := New(0)
	defer s.Close()

	s.Set("a", "1", 0)
	s.Delete("a")
	if _, ok := s.Get("a"); ok {
		t.Fatal("Get(a) after Delete = ok; want missing")
	}
}

func TestTTLExpiryLazy(t *testing.T) {
	s := New(0) // no janitor; rely on lazy expiry in Get
	defer s.Close()

	s.Set("a", "1", 20*time.Millisecond)
	if _, ok := s.Get("a"); !ok {
		t.Fatal("Get(a) before expiry = missing; want present")
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := s.Get("a"); ok {
		t.Fatal("Get(a) after expiry = ok; want missing")
	}
}

func TestJanitorEvicts(t *testing.T) {
	s := New(10 * time.Millisecond)
	defer s.Close()

	s.Set("a", "1", 5*time.Millisecond)
	// Allow several janitor sweeps to run after the entry expires.
	time.Sleep(60 * time.Millisecond)
	if n := s.Len(); n != 0 {
		t.Fatalf("Len after janitor sweep = %d; want 0", n)
	}
}
