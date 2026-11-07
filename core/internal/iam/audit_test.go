package iam

import "testing"

func TestMaskHidesSecretLookingKeysRecursively(t *testing.T) {
	in := map[string]any{
		"username":       "alice",
		"password":       "hunter2hunter2",
		"apiToken":       "abc",
		"nested":         map[string]any{"privateKey": "----", "name": "ok"},
		"recovery_codes": []string{"1", "2"},
	}
	out := Mask(in)
	if out["username"] != "alice" || out["password"] != "***" || out["apiToken"] != "***" || out["recovery_codes"] != "***" {
		t.Fatalf("mask = %v", out)
	}
	if n := out["nested"].(map[string]any); n["privateKey"] != "***" || n["name"] != "ok" {
		t.Fatalf("nested mask = %v", n)
	}
	if in["password"] != "hunter2hunter2" {
		t.Fatal("Mask must not mutate its input")
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery", TestPasswordParams)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := VerifyPassword("correct horse battery", h); !ok {
		t.Fatal("right password must verify")
	}
	if ok, _ := VerifyPassword("wrong", h); ok {
		t.Fatal("wrong password must not verify")
	}
	if _, err := VerifyPassword("x", "plain"); err == nil {
		t.Fatal("unknown hash format must error, not silently fail")
	}
}
