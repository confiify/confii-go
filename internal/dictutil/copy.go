package dictutil

import (
	"reflect"
	"time"
)

// DeepCopy returns a deep clone of a nested configuration map. Every
// reachable value is cloned through [DeepCopyValue] so the returned
// graph shares no mutable state with src. A nil src returns nil.
func DeepCopy(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	// Clone the root in one traversal so every reachable value shares the
	// same visited set. Calling DeepCopyValue separately for each top-level
	// entry breaks identity when two keys reference the same map/pointer and
	// makes a self-referential root map point at a second clone rather than
	// back to the returned root.
	return DeepCopyValue(src).(map[string]any)
}

// DeepCopyValue returns a deep clone of an arbitrary value. Maps,
// slices, arrays, pointers, and structs are recursively cloned.
// Scalars (string, numeric, bool, nil) are returned unchanged.
// Channels, function values, and unsafe pointers cannot be cloned and
// are returned by reference; they carry no Config-internal state that
// a caller could mutate through the returned interface.
//
// Cycles are detected via a (type, allocation address) visited set;
// self-referential pointer graphs terminate with shared substructure
// preserved across the clone.
func DeepCopyValue(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	visited := make(map[visitedKey]reflect.Value)
	cp := deepCopyRV(rv, visited)
	return cp.Interface()
}

// visitedKey identifies an already-cloned reference value so a second
// visit returns the original clone, preserving shared-substructure
// identity and breaking pointer cycles.
type visitedKey struct {
	typ reflect.Type
	ptr uintptr
}

// timeType is the cached reflect.Type of [time.Time]. Bit-copying a
// time.Time is safe — its *Location field points to globally shared,
// immutable state — and necessary, because the unexported wall/ext/loc
// fields cannot be assigned via reflect.Value.Set.
var timeType = reflect.TypeFor[time.Time]()

func deepCopyRV(v reflect.Value, visited map[visitedKey]reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		// No reachable Go-level state to copy; reference passthrough is
		// correct for the isolation contract.
		return v

	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		inner := deepCopyRV(v.Elem(), visited)
		out := reflect.New(v.Type()).Elem()
		out.Set(inner)
		return out

	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		key := visitedKey{typ: v.Type(), ptr: v.Pointer()}
		if existing, ok := visited[key]; ok {
			return existing
		}
		newPtr := reflect.New(v.Type().Elem())
		visited[key] = newPtr
		newPtr.Elem().Set(deepCopyRV(v.Elem(), visited))
		return newPtr

	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		key := visitedKey{typ: v.Type(), ptr: v.Pointer()}
		if existing, ok := visited[key]; ok {
			return existing
		}
		n := v.Len()
		out := reflect.MakeSlice(v.Type(), n, n)
		visited[key] = out
		// Primitive-element slices (notably []byte) have no nested
		// reference state, so a single reflect.Copy is bit-correct.
		if isPrimitiveKind(v.Type().Elem().Kind()) {
			reflect.Copy(out, v)
			return out
		}
		for i := range n {
			out.Index(i).Set(deepCopyRV(v.Index(i), visited))
		}
		return out

	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		if isPrimitiveKind(v.Type().Elem().Kind()) {
			reflect.Copy(out, v)
			return out
		}
		for i := range v.Len() {
			out.Index(i).Set(deepCopyRV(v.Index(i), visited))
		}
		return out

	case reflect.Map:
		if v.IsNil() {
			return v
		}
		key := visitedKey{typ: v.Type(), ptr: v.Pointer()}
		if existing, ok := visited[key]; ok {
			return existing
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		visited[key] = out
		iter := v.MapRange()
		for iter.Next() {
			kk := deepCopyRV(iter.Key(), visited)
			vv := deepCopyRV(iter.Value(), visited)
			out.SetMapIndex(kk, vv)
		}
		return out

	case reflect.Struct:
		// time.Time is bit-copied: its unexported loc *Location
		// references globally shared, immutable state, and reflect
		// cannot Set unexported fields.
		if v.Type() == timeType {
			out := reflect.New(v.Type()).Elem()
			out.Set(v)
			return out
		}
		// Bit-copy first to preserve unexported fields verbatim, then
		// overwrite each settable exported field with its deep clone.
		// Unexported reference fields remain shallow — reflect cannot
		// assign them — but the public surface a caller can mutate
		// through the struct's API is fully isolated.
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			cf := out.Field(i)
			if !cf.CanSet() {
				continue
			}
			cf.Set(deepCopyRV(f, visited))
		}
		return out
	default:
		// Scalar kinds have no reachable mutable state.
		return v
	}
}

// isPrimitiveKind reports whether values of kind k carry no reachable
// reference substructure. Slices and arrays of such kinds clone with a
// single reflect.Copy.
func isPrimitiveKind(k reflect.Kind) bool {
	switch k {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128,
		reflect.String:
		return true
	}
	return false
}
