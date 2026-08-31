package runtime

import (
	"reflect"
	"testing"
)

func TestSortedModelsIsStableAndGrouped(t *testing.T) {
	// CPA 每次返回的次序都不同，这里用打乱后的真实列表。
	input := []string{
		"gpt-5.6-luna",
		"gpt-5.4",
		"gpt-5.6-sol",
		"gpt-5.5",
		"codex-auto-review",
		"gpt-5.4-mini",
		"gpt-5.6-terra",
	}
	want := []string{
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.5",
		"gpt-5.6-luna",
		"gpt-5.6-sol",
		"gpt-5.6-terra",
		"codex-auto-review",
	}
	got := sortedModels(input)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedModels() = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(sortedModels(got), want) {
		t.Fatal("sortedModels is not idempotent")
	}
	if input[0] != "gpt-5.6-luna" {
		t.Fatal("sortedModels must not reorder the input slice")
	}
}

func TestSortedModelsComparesVersionsNumerically(t *testing.T) {
	got := sortedModels([]string{"gpt-5.10", "gpt-5.4", "gpt-5.2"})
	want := []string{"gpt-5.2", "gpt-5.4", "gpt-5.10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedModels() = %v, want %v", got, want)
	}
}
