package inventory

import (
	"testing"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
)

func TestDiff(t *testing.T) {
	previous := []v1alpha1.ResourceReference{
		{Group: "", Version: "v1", Kind: "ConfigMap", Namespace: "ns", Name: "old"},
		{Group: "", Version: "v1", Kind: "Secret", Namespace: "ns", Name: "kept"},
	}
	desired := []v1alpha1.ResourceReference{
		{Group: "", Version: "v1", Kind: "Secret", Namespace: "ns", Name: "kept"},
	}
	diff := Diff(previous, desired)
	if len(diff) != 1 || diff[0].Name != "old" {
		t.Fatalf("unexpected diff: %#v", diff)
	}
}
