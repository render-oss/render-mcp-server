package pointers

func From[T any](x T) *T {
	return &x
}
