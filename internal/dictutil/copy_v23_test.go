package dictutil

// V-03 (Wave 23) — Negative tests for the reflection-based DeepCopyValue.
//
// Each test below would fail against the pre-V-23 implementation, which
// handled only [map[string]any] and [[]any] in its switch and returned
// every other value unchanged. The reflection engine in copy.go now
// guarantees zero aliasing for typed slices, typed maps, byte slices,
// pointers, custom struct values, arrays, and self-referential pointer
// graphs.

import (
	"reflect"
	"testing"
	"time"
)

// V-03_a — []byte aliasing.
//
// Pre-V-23: DeepCopyValue([]byte("hello")) returned the SAME slice
// header. Mutating b[0] = 'X' bled through.
func TestDeepCopyValue_ByteSlice_NotAliased(t *testing.T) {
	src := []byte("hello")
	cp, ok := DeepCopyValue(src).([]byte)
	if !ok {
		t.Fatalf("DeepCopyValue did not return []byte, got %T", DeepCopyValue(src))
	}
	src[0] = 'X'
	if string(cp) != "hello" {
		t.Fatalf("V-03 byte-slice aliasing: cp = %q, want %q", string(cp), "hello")
	}
}

// V-03_b — typed []string aliasing.
func TestDeepCopyValue_StringSlice_NotAliased(t *testing.T) {
	src := []string{"a", "b", "c"}
	cp, ok := DeepCopyValue(src).([]string)
	if !ok {
		t.Fatalf("DeepCopyValue did not return []string, got %T", DeepCopyValue(src))
	}
	src[0] = "MUTATED"
	if cp[0] != "a" {
		t.Fatalf("V-03 string-slice aliasing: cp[0] = %q, want %q", cp[0], "a")
	}
}

// V-03_c — typed map[string]string aliasing.
func TestDeepCopyValue_TypedMap_NotAliased(t *testing.T) {
	src := map[string]string{"k": "v"}
	cp, ok := DeepCopyValue(src).(map[string]string)
	if !ok {
		t.Fatalf("DeepCopyValue did not return map[string]string, got %T", DeepCopyValue(src))
	}
	src["k"] = "MUTATED"
	if cp["k"] != "v" {
		t.Fatalf("V-03 typed-map aliasing: cp[k] = %q, want %q", cp["k"], "v")
	}
}

// V-03_d — pointer aliasing.
func TestDeepCopyValue_Pointer_NotAliased(t *testing.T) {
	x := 42
	src := &x
	cp, ok := DeepCopyValue(src).(*int)
	if !ok {
		t.Fatalf("DeepCopyValue did not return *int, got %T", DeepCopyValue(src))
	}
	if cp == src {
		t.Fatalf("V-03 pointer aliasing: cp and src share allocation")
	}
	*src = 999
	if *cp != 42 {
		t.Fatalf("V-03 pointer aliasing: *cp = %d, want %d", *cp, 42)
	}
}

// V-03_e — struct with reference-typed exported fields.
type vuln03Struct struct {
	Name string
	Tags []string
	M    map[string]int
}

func TestDeepCopyValue_Struct_NotAliased(t *testing.T) {
	src := vuln03Struct{
		Name: "alpha",
		Tags: []string{"a", "b"},
		M:    map[string]int{"x": 1},
	}
	cp, ok := DeepCopyValue(src).(vuln03Struct)
	if !ok {
		t.Fatalf("DeepCopyValue did not return vuln03Struct, got %T", DeepCopyValue(src))
	}
	src.Tags[0] = "MUTATED"
	src.M["x"] = 999
	if cp.Tags[0] != "a" {
		t.Fatalf("V-03 struct.slice aliasing: cp.Tags[0] = %q, want %q", cp.Tags[0], "a")
	}
	if cp.M["x"] != 1 {
		t.Fatalf("V-03 struct.map aliasing: cp.M[x] = %d, want %d", cp.M["x"], 1)
	}
}

// V-03_f — pointer-cycle termination.
type vuln03Node struct {
	Name string
	Next *vuln03Node
}

func TestDeepCopyValue_PointerCycle_Terminates(t *testing.T) {
	a := &vuln03Node{Name: "a"}
	b := &vuln03Node{Name: "b"}
	a.Next = b
	b.Next = a // cycle

	done := make(chan struct{})
	go func() {
		defer close(done)
		cp, ok := DeepCopyValue(a).(*vuln03Node)
		if !ok || cp == nil {
			t.Errorf("DeepCopyValue did not return *vuln03Node, got %T", DeepCopyValue(a))
			return
		}
		if cp == a {
			t.Errorf("V-03 cycle: cp shares allocation with src")
		}
		if cp.Next == nil {
			t.Errorf("V-03 cycle: cp.Next is nil — cycle not preserved")
		}
		if cp.Next.Next != cp {
			t.Errorf("V-03 cycle: cp.Next.Next must point back to cp (cycle preserved)")
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("V-03 cycle: DeepCopyValue did not terminate within 2s")
	}
}

// V-03_g — nested map[string]any with []any preserves the F-G10 contract.
// This is the only pre-V-23 path that worked; included here to confirm
// the reflection engine's handling of the JSON/YAML decoder shape is
// behaviourally identical.
func TestDeepCopyValue_MapStringAny_Backwards(t *testing.T) {
	src := map[string]any{
		"k": []any{"a", "b"},
		"m": map[string]any{"x": 1},
	}
	cp, ok := DeepCopyValue(src).(map[string]any)
	if !ok {
		t.Fatalf("DeepCopyValue did not return map[string]any, got %T", DeepCopyValue(src))
	}
	cpInner := cp["k"].([]any)
	srcInner := src["k"].([]any)
	srcInner[0] = "MUTATED"
	if cpInner[0] != "a" {
		t.Fatalf("V-03 map[string]any: cp[k][0] = %v, want %v", cpInner[0], "a")
	}
}

// V-03_h — array (not slice) deep-copy.
func TestDeepCopyValue_Array_NotAliased(t *testing.T) {
	type arrType [3]string
	src := arrType{"x", "y", "z"}
	cp, ok := DeepCopyValue(src).(arrType)
	if !ok {
		t.Fatalf("DeepCopyValue did not return arrType, got %T", DeepCopyValue(src))
	}
	// Arrays are value types, but verify the contract holds.
	if !reflect.DeepEqual(cp, src) {
		t.Fatalf("V-03 array: cp != src after copy")
	}
}

// V-03_i — time.Time is bit-copied safely (its loc *Location field
// points at globally-shared, immutable state).
func TestDeepCopyValue_TimeTime_Roundtrip(t *testing.T) {
	now := time.Now()
	cp, ok := DeepCopyValue(now).(time.Time)
	if !ok {
		t.Fatalf("DeepCopyValue did not return time.Time, got %T", DeepCopyValue(now))
	}
	if !cp.Equal(now) {
		t.Fatalf("V-03 time.Time: cp != now (Equal failed)")
	}
}

// V-03_j — nil passthrough.
func TestDeepCopyValue_Nil_PassThrough(t *testing.T) {
	if got := DeepCopyValue(nil); got != nil {
		t.Fatalf("DeepCopyValue(nil) = %v, want nil", got)
	}
}

// A single DeepCopy traversal must preserve shared substructure across
// top-level keys. Pre-fix, DeepCopy called DeepCopyValue independently for
// every entry, so aliases inside the source graph became unrelated clones.
func TestDeepCopy_TopLevelSharedMapIdentityPreserved(t *testing.T) {
	shared := map[string]any{"value": "original"}
	src := map[string]any{"left": shared, "right": shared}

	cp := DeepCopy(src)
	left := cp["left"].(map[string]any)
	right := cp["right"].(map[string]any)
	left["value"] = "changed-through-left"

	if got := right["value"]; got != "changed-through-left" {
		t.Fatalf("shared identity lost: right[value] = %v", got)
	}
	if got := shared["value"]; got != "original" {
		t.Fatalf("clone aliases source: shared[value] = %v", got)
	}
}

// A root map may point to itself. The clone must terminate and the self edge
// must point back to the returned root, not to a second root-shaped clone.
func TestDeepCopy_SelfReferentialRootMapPreserved(t *testing.T) {
	src := map[string]any{"value": "original"}
	src["self"] = src

	cp := DeepCopy(src)
	self := cp["self"].(map[string]any)
	cp["value"] = "changed-through-root"

	if got := self["value"]; got != "changed-through-root" {
		t.Fatalf("self edge does not point to cloned root: self[value] = %v", got)
	}
	if got := src["value"]; got != "original" {
		t.Fatalf("clone aliases source root: src[value] = %v", got)
	}
}
