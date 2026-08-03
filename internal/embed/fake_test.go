package embed

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestFakeProvider_IsDeterministic(t *testing.T) {
	t.Parallel()

	a := NewFakeProvider("fake", 16, 42)
	b := NewFakeProvider("fake", 16, 42)

	v1, err := a.Embed(context.Background(), []string{"hello world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	v2, err := b.Embed(context.Background(), []string{"hello world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			t.Fatalf("component %d differs across instances: %v vs %v", i, v1[0][i], v2[0][i])
		}
	}

	// Repeat calls on the same instance must also agree.
	v3, _ := a.Embed(context.Background(), []string{"hello world"})
	for i := range v1[0] {
		if v1[0][i] != v3[0][i] {
			t.Fatalf("component %d not stable across calls", i)
		}
	}
}

func TestFakeProvider_DifferentSeedsDiffer(t *testing.T) {
	t.Parallel()

	a := NewFakeProvider("fake", 16, 1)
	b := NewFakeProvider("fake", 16, 2)

	v1, _ := a.Embed(context.Background(), []string{"same text"})
	v2, _ := b.Embed(context.Background(), []string{"same text"})

	identical := true
	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			identical = false
			break
		}
	}
	if identical {
		t.Error("different seeds produced identical vectors")
	}
}

func TestFakeProvider_VectorsAreNormalised(t *testing.T) {
	t.Parallel()

	f := NewFakeProvider("fake", 32, 7)
	vecs, err := f.Embed(context.Background(), []string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	for i, v := range vecs {
		var sum float64
		for _, c := range v {
			sum += float64(c) * float64(c)
		}
		norm := math.Sqrt(sum)
		if math.Abs(norm-1.0) > 1e-5 {
			t.Errorf("vector %d has L2 norm %v, want 1.0", i, norm)
		}
	}
}

func TestFakeProvider_ShapeAndMetadata(t *testing.T) {
	t.Parallel()

	f := NewFakeProvider("my-model", 12, 3)
	if f.ModelID() != "my-model" {
		t.Errorf("ModelID() = %q", f.ModelID())
	}
	if f.Dimensions() != 12 {
		t.Errorf("Dimensions() = %d, want 12", f.Dimensions())
	}

	vecs, err := f.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("got %d vectors, want 3", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 12 {
			t.Errorf("vector %d has width %d, want 12", i, len(v))
		}
	}

	if f.Calls() != 1 {
		t.Errorf("Calls() = %d, want 1", f.Calls())
	}
	if f.Embedded() != 3 {
		t.Errorf("Embedded() = %d, want 3", f.Embedded())
	}
	if err := f.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestFakeProvider_EmptyInput(t *testing.T) {
	t.Parallel()

	f := NewFakeProvider("fake", 8, 1)
	vecs, err := f.Embed(context.Background(), nil)
	if err != nil || vecs != nil {
		t.Fatalf("Embed(nil) = %v, %v; want nil, nil", vecs, err)
	}
	if f.Calls() != 0 {
		t.Errorf("empty input counted as a call")
	}
}

func TestFakeProvider_InjectedErrors(t *testing.T) {
	t.Parallel()

	f := NewFakeProvider("fake", 8, 1)
	f.EmbedErr = ErrFakeFailure
	if _, err := f.Embed(context.Background(), []string{"x"}); !errors.Is(err, ErrFakeFailure) {
		t.Errorf("Embed err = %v, want ErrFakeFailure", err)
	}

	f2 := NewFakeProvider("fake", 8, 1)
	f2.HealthErr = ErrUnavailable
	if err := f2.Health(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Health err = %v, want ErrUnavailable", err)
	}
}

func TestFakeProvider_RespectsContext(t *testing.T) {
	t.Parallel()

	f := NewFakeProvider("fake", 8, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := f.Embed(ctx, []string{"x"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Embed with cancelled ctx = %v, want context.Canceled", err)
	}
	if err := f.Health(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Health with cancelled ctx = %v, want context.Canceled", err)
	}
}

func TestFakeProvider_HookCanBlockAndFail(t *testing.T) {
	t.Parallel()

	f := NewFakeProvider("fake", 8, 1)
	hookErr := errors.New("hook says no")
	f.Hook = func(ctx context.Context, texts []string) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
		return hookErr
	}

	if _, err := f.Embed(context.Background(), []string{"x"}); !errors.Is(err, hookErr) {
		t.Errorf("Embed err = %v, want %v", err, hookErr)
	}
}

func TestFakeProvider_DefaultsForInvalidArgs(t *testing.T) {
	t.Parallel()

	f := NewFakeProvider("", 0, 0)
	if f.Dimensions() <= 0 {
		t.Error("non-positive dim should fall back to a usable default")
	}
	if f.ModelID() == "" {
		t.Error("empty model should fall back to a default label")
	}
}

// TestFakeProvider_SatisfiesProvider is a compile-time assertion made explicit.
func TestFakeProvider_SatisfiesProvider(t *testing.T) {
	t.Parallel()
	var p Provider = NewFakeProvider("fake", 4, 1)
	if p.Dimensions() != 4 {
		t.Errorf("Dimensions() = %d", p.Dimensions())
	}
}
