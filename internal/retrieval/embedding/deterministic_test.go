package embedding

import (
	"context"
	"reflect"
	"testing"
)

func TestDeterministicProviderReturnsStableVectors(t *testing.T) {
	provider, err := NewDeterministic(8)
	if err != nil {
		t.Fatalf("NewDeterministic() error = %v", err)
	}
	first, err := provider.Embed(context.Background(), []string{"checkout timeout"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	second, _ := provider.Embed(context.Background(), []string{"checkout timeout"})
	if len(first) != 1 || len(first[0]) != 8 || !reflect.DeepEqual(first, second) {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}
