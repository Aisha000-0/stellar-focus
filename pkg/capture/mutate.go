package capture

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
)

// Mutation is one corruption applied to captured bytes. The four shapes are
// the ones a fixture parser meets most: a bit flip somewhere, a byte zeroed,
// a byte set to 0xFF everywhere, and a truncated record. Every shape is
// deterministic for a given input and step number, so a mutant can be
// regenerated and its expected parse behaviour asserted in CI without
// committing the mutant itself.
type Mutation struct {
	// Op names the corruption. It is part of the deterministic choice, not
	// a knob the caller turns.
	Op string `json:"op"`
	// Offset is the byte index the corruption touched, counted from the
	// start of the decoded XDR.
	Offset int `json:"offset"`
}

// Mutate returns a corrupted variation of the base64 XDR in b64.
//
// seed starts the corruption stream and steps counts how many corruptions to
// apply; the same inputs always give the same mutant, and reporting what was
// touched — see Mutations — keeps a resilience test's name meaningful. The
// result is still base64; whether it still parses is the question a
// resilience test asks, which is why nothing here attempts to stay or skip
// field boundaries on the caller's behalf.
//
// A zero-length payload and an invalid base64 string are both errors rather
// than a no-op: a mutator that quietly passes its input through would make a
// resilience suite appear to exercise parsers it never reached.
func Mutate(b64 string, seed uint64, steps int) (mutant string, mutations []Mutation, err error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", nil, fmt.Errorf("mutating: input is not valid base64 XDR: %w", err)
	}
	if len(data) == 0 {
		return "", nil, fmt.Errorf("mutating: nothing to corrupt (payload is empty)")
	}
	if steps < 0 {
		return "", nil, fmt.Errorf("mutating: steps must not be negative")
	}
	if steps == 0 {
		return base64.StdEncoding.EncodeToString(data), nil, nil
	}

	for step := 0; step < steps; step++ {
		off, op := pick(data, seed, step)
		mut := Mutation{Op: opName(op), Offset: off}
		switch op {
		case 0:
			data[off] ^= 0xFF
		case 1:
			data[off] = 0x00
		case 2:
			data[off] = 0xFF
		case 3:
			// Truncate after the chosen byte. Repeated truncation moves
			// the record shorter, so mutants from the same seed with more
			// steps read less and less like the original — the shape a
			// parser meets on cut-off input.
			if off+1 < len(data) {
				data = data[:off+1]
			}
		}
		mutations = append(mutations, mut)
	}

	return base64.StdEncoding.EncodeToString(data), mutations, nil
}

// Mutations reports what Mutate touched, so a resilience test can name its
// mutants exactly rather than diffing whole payloads.
func Mutations(b64 string, seed uint64, steps int) ([]Mutation, error) {
	_, mutations, err := Mutate(b64, seed, steps)
	if err != nil {
		return nil, err
	}
	return mutations, nil
}

// pick chooses one corruption for one step from the seed stream.
func pick(data []byte, seed uint64, step int) (int, int) {
	h := sha256.New()
	seedBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(seedBytes, seed)
	stepBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(stepBytes, uint32(step))
	_, _ = h.Write(seedBytes)
	_, _ = h.Write(stepBytes)
	sum := h.Sum(nil)

	op := int(sum[0] % 4)
	if len(data) < 2 {
		// A one-byte payload only supports in-place edits; truncating it
		// would give an empty record, which Mutate already refuses.
		return 0, op % 3
	}
	off := int(binary.BigEndian.Uint32(sum[1:5]) % uint32(len(data)))
	return off, op
}

// opName makes a mutation's op readable in a fixture name.
func opName(op int) string {
	switch op {
	case 0:
		return "bitflip"
	case 1:
		return "zero"
	case 2:
		return "ones"
	case 3:
		return "truncate"
	}
	return "unknown"
}
