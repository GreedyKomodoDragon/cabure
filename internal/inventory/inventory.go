package inventory

import (
	"sort"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
)

func Normalize(items []v1alpha1.ResourceReference) []v1alpha1.ResourceReference {
	out := append([]v1alpha1.ResourceReference(nil), items...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key() < out[j].Key()
	})
	return out
}

func Equal(a, b []v1alpha1.ResourceReference) bool {
	if len(a) != len(b) {
		return false
	}
	a = Normalize(a)
	b = Normalize(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func Diff(previous, desired []v1alpha1.ResourceReference) []v1alpha1.ResourceReference {
	prev := make(map[string]v1alpha1.ResourceReference, len(previous))
	for _, ref := range previous {
		prev[ref.Key()] = ref
	}
	for _, ref := range desired {
		delete(prev, ref.Key())
	}
	out := make([]v1alpha1.ResourceReference, 0, len(prev))
	for _, ref := range prev {
		out = append(out, ref)
	}
	return Normalize(out)
}
