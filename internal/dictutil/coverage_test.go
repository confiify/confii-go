package dictutil

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestNormalizeKeys_AllSupportedKeyKinds(t *testing.T) {
	tests := []struct {
		key  any
		want string
	}{
		{"text", "text"}, {false, "false"},
		{int(-1), "-1"}, {int8(-2), "-2"}, {int16(-3), "-3"},
		{int32(-4), "-4"}, {int64(-5), "-5"},
		{uint(1), "1"}, {uint8(2), "2"}, {uint16(3), "3"},
		{uint32(4), "4"}, {uint64(5), "5"}, {uintptr(6), "6"},
		{float32(1.25), "1.25"}, {float64(2.5), "2.5"},
	}
	for _, tc := range tests {
		t.Run(reflect.TypeOf(tc.key).String(), func(t *testing.T) {
			got, err := coerceMapKey(tc.key)
			if err != nil || got != tc.want {
				t.Fatalf("coerceMapKey(%T(%v)) = %q, %v; want %q, nil", tc.key, tc.key, got, err, tc.want)
			}
		})
	}
}

func TestNormalizeKeys_ErrorDetailsAndNestedValueFailure(t *testing.T) {
	_, err := NormalizeKeys(map[any]any{"outer": map[any]any{math.NaN(): "bad"}})
	if err == nil {
		t.Fatal("expected nested coercion error")
	}
	var coercion *KeyCoercionError
	if !errors.As(err, &coercion) || !strings.Contains(err.Error(), "NaN cannot be a map key") {
		t.Fatalf("nested coercion error = %T(%v)", err, err)
	}
	if got := (&KeyCoercionError{Type: "custom", Reason: "unsupported"}).Error(); !strings.Contains(got, "custom") || !strings.Contains(got, "unsupported") {
		t.Fatalf("unexpected coercion error: %q", got)
	}
	float32NaN := math.Float32frombits(0x7fc00000)
	if _, err := coerceMapKey(float32NaN); err == nil || !strings.Contains(err.Error(), "NaN") {
		t.Fatalf("float32 NaN error = %v", err)
	}
}

func TestDeepCopyValue_RemainingReflectionKinds(t *testing.T) {
	if got := DeepCopy(nil); got != nil {
		t.Fatalf("DeepCopy(nil) = %#v", got)
	}
	if deepCopyRV(reflect.Value{}, map[visitedKey]reflect.Value{}).IsValid() {
		t.Fatal("invalid reflect.Value should remain invalid")
	}

	var nilInterface any
	interfaceValue := reflect.ValueOf(&nilInterface).Elem()
	if got := deepCopyRV(interfaceValue, map[visitedKey]reflect.Value{}); !got.IsNil() {
		t.Fatal("nil interface should remain nil")
	}
	var nilPointer *int
	var nilSlice []string
	var nilMap map[string]int
	for _, value := range []any{nilPointer, nilSlice, nilMap} {
		if got := DeepCopyValue(value); !reflect.ValueOf(got).IsNil() {
			t.Fatalf("typed nil %T should remain nil", value)
		}
	}

	ch := make(chan int)
	fn := func() {}
	x := 1
	ptr := unsafe.Pointer(&x)
	if DeepCopyValue(ch) != ch || reflect.ValueOf(DeepCopyValue(fn)).Pointer() != reflect.ValueOf(fn).Pointer() || DeepCopyValue(ptr) != ptr {
		t.Fatal("non-cloneable references must pass through unchanged")
	}

	nested := [1]*int{&x}
	clone := DeepCopyValue(nested).([1]*int)
	if clone[0] == nested[0] || *clone[0] != x {
		t.Fatal("array pointer element was not deeply cloned")
	}

	type privateFields struct {
		Exported []string
		hidden   []string
	}
	src := privateFields{Exported: []string{"public"}, hidden: []string{"private"}}
	got := DeepCopyValue(src).(privateFields)
	if &got.Exported[0] == &src.Exported[0] || got.hidden[0] != "private" {
		t.Fatal("struct fields were not copied according to visibility contract")
	}
}

func TestDeepCopyValue_PreservesSliceCyclesAndSharedPointers(t *testing.T) {
	cycle := make([]any, 1)
	cycle[0] = cycle
	clone := DeepCopyValue(cycle).([]any)
	cloneCycle := clone[0].([]any)
	cloneCycle[0] = "changed"
	if cloneCycle[0] != "changed" {
		t.Fatal("slice cycle was not preserved independently")
	}
	if _, ok := cycle[0].([]any)[0].([]any); !ok {
		t.Fatal("mutating cloned cycle changed source cycle")
	}

	x := 7
	src := []*int{&x, &x}
	got := DeepCopyValue(src).([]*int)
	if got[0] != got[1] || got[0] == src[0] {
		t.Fatal("shared pointer identity was not preserved")
	}
}
