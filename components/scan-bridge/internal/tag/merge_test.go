package tag

import (
	"slices"
	"testing"
)

func TestMerge(t *testing.T) {
	t.Parallel()

	defaults := []int{3, 7, 9}
	caller := []int{21}

	t.Run("no_caller_tags_returns_defaults_copy", func(t *testing.T) {
		t.Parallel()

		defaultsCopy := []int{3, 7, 9}
		got := Merge(defaultsCopy, StrategyAdd, nil, "")
		want := []int{3, 7, 9}
		if !slices.Equal(got, want) {
			t.Fatalf("Merge = %v, want %v", got, want)
		}

		// No aliasing: mutating the input defaults after the call must not
		// change the returned slice.
		defaultsCopy[0] = 999
		if !slices.Equal(got, want) {
			t.Fatalf("Merge result aliased input defaults: got %v, want %v", got, want)
		}
	})

	t.Run("add_union_deduplicated", func(t *testing.T) {
		t.Parallel()

		got := Merge(defaults, StrategyAdd, caller, "")
		want := []int{3, 7, 9, 21}
		if !slices.Equal(got, want) {
			t.Fatalf("Merge = %v, want %v", got, want)
		}
	})

	t.Run("override_caller_only_ignores_defaults", func(t *testing.T) {
		t.Parallel()

		got := Merge(defaults, StrategyOverride, caller, "")
		want := []int{21}
		if !slices.Equal(got, want) {
			t.Fatalf("Merge = %v, want %v", got, want)
		}
	})

	t.Run("caller_override_overrides_profile_add", func(t *testing.T) {
		t.Parallel()

		got := Merge(defaults, StrategyAdd, caller, StrategyOverride)
		want := []int{21}
		if !slices.Equal(got, want) {
			t.Fatalf("Merge = %v, want %v", got, want)
		}
	})

	t.Run("caller_add_overrides_profile_override", func(t *testing.T) {
		t.Parallel()

		got := Merge(defaults, StrategyOverride, caller, StrategyAdd)
		want := []int{3, 7, 9, 21}
		if !slices.Equal(got, want) {
			t.Fatalf("Merge = %v, want %v", got, want)
		}
	})

	t.Run("remove_drops_matching_default", func(t *testing.T) {
		t.Parallel()

		got := Merge(defaults, StrategyAdd, []int{7}, StrategyRemove)
		want := []int{3, 9}
		if !slices.Equal(got, want) {
			t.Fatalf("Merge = %v, want %v", got, want)
		}
	})

	t.Run("remove_nonexistent_is_noop", func(t *testing.T) {
		t.Parallel()

		got := Merge(defaults, StrategyAdd, []int{99}, StrategyRemove)
		want := []int{3, 7, 9}
		if !slices.Equal(got, want) {
			t.Fatalf("Merge = %v, want %v", got, want)
		}
	})

	t.Run("add_deduplicates_against_defaults", func(t *testing.T) {
		t.Parallel()

		got := Merge(defaults, StrategyAdd, []int{3, 21}, "")
		want := []int{3, 7, 9, 21}
		if !slices.Equal(got, want) {
			t.Fatalf("Merge = %v, want %v", got, want)
		}
	})

	t.Run("unknown_strategy_behaves_like_add", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Merge panicked: %v", r)
			}
		}()

		got := Merge(defaults, Strategy("bogus"), caller, "")
		want := []int{3, 7, 9, 21}
		if !slices.Equal(got, want) {
			t.Fatalf("Merge = %v, want %v", got, want)
		}
	})
}
