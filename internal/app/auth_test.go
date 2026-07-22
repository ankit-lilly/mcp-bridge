package app

import "testing"

func TestDeriveCallbackPort_Stable(t *testing.T) {
	p1 := deriveCallbackPort("abc123")
	p2 := deriveCallbackPort("abc123")
	if p1 != p2 {
		t.Fatal("same hash must yield same port")
	}
	if p1 < derivedCallbackPortBase || p1 >= derivedCallbackPortBase+derivedCallbackPortSpan {
		t.Fatalf("port out of range: %d", p1)
	}
}

func TestDeriveCallbackPort_DifferentInputsStayInRange(t *testing.T) {
	p1 := deriveCallbackPort("hash1")
	p2 := deriveCallbackPort("hash2")
	if p1 < derivedCallbackPortBase || p1 >= derivedCallbackPortBase+derivedCallbackPortSpan {
		t.Fatalf("port out of range: %d", p1)
	}
	if p2 < derivedCallbackPortBase || p2 >= derivedCallbackPortBase+derivedCallbackPortSpan {
		t.Fatalf("port out of range: %d", p2)
	}
}
