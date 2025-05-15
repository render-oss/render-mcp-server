package pointers

import "time"

func From[T any](x T) *T {
	return &x
}

func FromArray[T any](x []T) *[]T {
	if len(x) == 0 {
		return nil
	}

	return &x
}

func ValueOrDefault[T any](x *T, def T) T {
	if x == nil {
		return def
	}
	return *x
}

func PointerValueIfNotEmptyString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func StringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TimeValue(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
