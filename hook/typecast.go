package hook

import "github.com/qualitycoe/confii-go/internal/typecoerce"

// NewTypeCastHook returns a hook that converts string values to their
// most appropriate Go type (bool, int, float64).
func NewTypeCastHook() Func {
	return func(_ string, value any) any {
		s, ok := value.(string)
		if !ok {
			return value
		}
		return typecoerce.ParseScalar(s, false)
	}
}
