package pointers

import "testing"

func TestFromFloat(t *testing.T) {
	v := 3.14
	p := FromFloat(v)
	if *p != v {
		t.Fatalf("expected %f, got %f", v, *p)
	}
}

func TestFromString(t *testing.T) {
	v := "hello"
	p := FromString(v)
	if *p != v {
		t.Fatalf("expected %s, got %s", v, *p)
	}
}

func TestFromInt(t *testing.T) {
	v := 42
	p := FromInt(v)
	if *p != v {
		t.Fatalf("expected %d, got %d", v, *p)
	}
}

func TestFromBool(t *testing.T) {
	v := true
	p := FromBool(v)
	if *p != v {
		t.Fatalf("expected %v, got %v", v, *p)
	}
}
