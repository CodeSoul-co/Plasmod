package segmentstore

type Set[T comparable] struct {
	values map[T]struct{}
}

func NewSet[T comparable]() *Set[T] {
	return &Set[T]{values: map[T]struct{}{}}
}

func (s *Set[T]) Insert(value T) {
	s.values[value] = struct{}{}
}

func (s *Set[T]) Collect() []T {
	out := make([]T, 0, len(s.values))
	for value := range s.values {
		out = append(out, value)
	}
	return out
}
