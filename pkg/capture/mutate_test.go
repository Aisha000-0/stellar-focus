package capture

import (
	"encoding/base64"
	"testing"
)

const samplePayload = "AAAAAAAAAGQAAAAAAAAA+owAAAAAAAAAAGQAAAAAAAAAAGqZAAAAAAAD6jAAAABwAAAAAAAAAAAAAAAAAAAAEBAAAAAAAAALNuO5b7acqW1sVPZ/JTluF8v93g=="

func TestMutateDeterministic(t *testing.T) {
	one, mutOne, err := Mutate(samplePayload, 7, 3)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	two, mutTwo, err := Mutate(samplePayload, 7, 3)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}

	if one != two {
		t.Error("same seed and steps must produce identical mutants")
	}
	if len(mutOne) != len(mutTwo) {
		t.Fatalf("mutation reports differ in size: %d vs %d", len(mutOne), len(mutTwo))
	}
	for i := range mutOne {
		if mutOne[i] != mutTwo[i] {
			t.Errorf("mutation %d differs: %+v vs %+v", i, mutOne[i], mutTwo[i])
		}
	}
}

func TestMutateChangesPayload(t *testing.T) {
	mutant, muts, err := Mutate(samplePayload, 42, 2)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}

	if mutant == samplePayload {
		t.Error("two steps of corruption must change the payload")
	}
	if len(muts) != 2 {
		t.Fatalf("the report must name exactly what was applied: %d entries, %+v", len(muts), muts)
	}

	for _, m := range muts {
		if m.Op == "unknown" {
			t.Errorf("corruption op names must be resolved: %+v", m)
		}
		if m.Offset < 0 {
			t.Errorf("offsets are valid byte indexes: %+v", m)
		}
	}
}

func TestMutateZeroStepsIsNoOp(t *testing.T) {
	mutant, muts, err := Mutate(samplePayload, 1, 0)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}

	if mutant != samplePayload {
		t.Error("zero steps must return the input unchanged")
	}
	if len(muts) != 0 {
		t.Errorf("zero steps produce no mutation entries, got %+v", muts)
	}
}

func TestMutateDifferentSeedsDiverge(t *testing.T) {
	a, _, err := Mutate(samplePayload, 1, 4)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	b, _, err := Mutate(samplePayload, 99, 4)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}

	if a == b {
		t.Error("different seeds should give different mutants")
	}
}

func TestMutateErrors(t *testing.T) {
	t.Run("invalid base64", func(t *testing.T) {
		_, _, err := Mutate("!!!not-base64!!!", 1, 1)
		if err == nil || !contains(err.Error(), "not valid base64") {
			t.Fatalf("want a base64 error, got: %v", err)
		}
	})

	t.Run("empty payload", func(t *testing.T) {
		empty := base64.StdEncoding.EncodeToString(nil)
		_, _, err := Mutate(empty, 1, 1)
		if err == nil || !contains(err.Error(), "nothing to corrupt") {
			t.Fatalf("want an empty-payload error, got: %v", err)
		}
	})

	t.Run("negative steps", func(t *testing.T) {
		_, _, err := Mutate(samplePayload, 1, -1)
		if err == nil || !contains(err.Error(), "must not be negative") {
			t.Fatalf("want a negative-steps error, got: %v", err)
		}
	})
}

func TestMutationsReportOnly(t *testing.T) {
	muts, err := Mutations(samplePayload, 3, 2)
	if err != nil {
		t.Fatalf("Mutations: %v", err)
	}
	if len(muts) != 2 {
		t.Fatalf("want two mutation entries, got %+v", muts)
	}
}
