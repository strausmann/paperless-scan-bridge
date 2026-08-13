package destinations

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
)

// fakeDestination is this package's own conformance fake for exercising
// Register/Build. The Paperless module (a later task) gets its own
// httptest.Server-backed tests instead of reusing this fake.
type fakeDestination struct {
	name       string
	deliverErr error
	calls      []fakeDeliverCall
}

type fakeDeliverCall struct {
	doc  Document
	meta Metadata
	cfg  ProfileDestinationConfig
}

func (f *fakeDestination) Name() string { return f.name }

func (f *fakeDestination) Deliver(_ context.Context, doc Document, meta Metadata, cfg ProfileDestinationConfig) error {
	f.calls = append(f.calls, fakeDeliverCall{doc: doc, meta: meta, cfg: cfg})
	return f.deliverErr
}

// newFakeConstructor returns a Constructor that either fails with
// ctorErr or succeeds returning dest, ignoring the cfg/secrets it is
// called with (the fake only needs to prove Build wires them through
// and reacts to the constructor's own outcome).
func newFakeConstructor(dest *fakeDestination, ctorErr error) Constructor {
	return func(ProfileDestinationConfig, config.SecretResolver) (Destination, error) {
		if ctorErr != nil {
			return nil, ctorErr
		}
		return dest, nil
	}
}

// uniqueName derives a registry name from the running (sub)test name,
// so parallel subtests never collide on the shared package-level
// registry regardless of execution order.
func uniqueName(t *testing.T) string {
	t.Helper()
	return "test-" + strings.ReplaceAll(t.Name(), "/", "-")
}

func TestRegisterAndBuild(t *testing.T) {
	t.Parallel()

	t.Run("build_resolves_registered_constructor", func(t *testing.T) {
		t.Parallel()

		name := uniqueName(t)
		fake := &fakeDestination{name: name}
		Register(name, newFakeConstructor(fake, nil))

		got, err := Build(name, ProfileDestinationConfig{Target: name}, config.SecretResolver{})
		if err != nil {
			t.Fatalf("Build(%q) returned error: %v", name, err)
		}
		if got.Name() != name {
			t.Fatalf("Build(%q).Name() = %q, want %q", name, got.Name(), name)
		}
	})

	t.Run("build_unknown_name_returns_error", func(t *testing.T) {
		t.Parallel()

		name := uniqueName(t) // deliberately never registered

		_, err := Build(name, ProfileDestinationConfig{Target: name}, config.SecretResolver{})
		if err == nil {
			t.Fatalf("Build(%q) = nil error, want error for unregistered name", name)
		}
		if !errors.Is(err, ErrUnknownDestination) {
			t.Fatalf("Build(%q) error = %v, want errors.Is(err, ErrUnknownDestination)", name, err)
		}
	})

	t.Run("build_propagates_constructor_error", func(t *testing.T) {
		t.Parallel()

		name := uniqueName(t)
		ctorErr := errors.New("boom: bad destination config")
		Register(name, newFakeConstructor(nil, ctorErr))

		_, err := Build(name, ProfileDestinationConfig{Target: name}, config.SecretResolver{})
		if err == nil {
			t.Fatalf("Build(%q) = nil error, want the constructor's error wrapped", name)
		}
		if !errors.Is(err, ctorErr) {
			t.Fatalf("Build(%q) error = %v, want errors.Is(err, ctorErr)", name, err)
		}
	})

	t.Run("build_passes_cfg_and_secrets_through_to_constructor", func(t *testing.T) {
		t.Parallel()

		// config.SecretResolver holds an unexported lookupEnv func
		// field, so it is not comparable with == and its fields are
		// unexported outside package config. Prove pass-through
		// behaviourally instead: the constructor resolves a probe
		// secret through whatever resolver Build handed it, and the
		// test controls what that resolver returns.
		name := uniqueName(t)
		var gotCfg ProfileDestinationConfig
		var gotSecretValue string
		var gotSecretErr error
		ctor := func(cfg ProfileDestinationConfig, secrets config.SecretResolver) (Destination, error) {
			gotCfg = cfg
			gotSecretValue, gotSecretErr = secrets.Resolve("probe_secret")
			return &fakeDestination{name: name}, nil
		}
		Register(name, ctor)

		wantCfg := ProfileDestinationConfig{
			Target:       name,
			StorageFirst: true,
			Config:       map[string]any{"base_url": "https://paperless.example.com"},
		}
		wantSecrets := config.NewSecretResolver("", func(key string) (string, bool) {
			if key == "PROBE_SECRET" {
				return "probe-value", true
			}
			return "", false
		})

		if _, err := Build(name, wantCfg, wantSecrets); err != nil {
			t.Fatalf("Build(%q) returned error: %v", name, err)
		}
		if gotCfg.Target != wantCfg.Target || gotCfg.StorageFirst != wantCfg.StorageFirst {
			t.Fatalf("constructor received cfg = %+v, want %+v", gotCfg, wantCfg)
		}
		if gotCfg.Config["base_url"] != wantCfg.Config["base_url"] {
			t.Fatalf("constructor received cfg.Config = %+v, want %+v", gotCfg.Config, wantCfg.Config)
		}
		if gotSecretErr != nil {
			t.Fatalf("secrets.Resolve via constructor returned error: %v", gotSecretErr)
		}
		if gotSecretValue != "probe-value" {
			t.Fatalf("secrets.Resolve via constructor = %q, want %q", gotSecretValue, "probe-value")
		}
	})

	t.Run("register_duplicate_name_panics", func(t *testing.T) {
		t.Parallel()

		name := uniqueName(t)
		fake := &fakeDestination{name: name}
		Register(name, newFakeConstructor(fake, nil))

		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("Register(%q) second call did not panic", name)
			}
		}()
		Register(name, newFakeConstructor(fake, nil))
	})

	t.Run("register_empty_name_panics", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Fatal(`Register("", ...) did not panic`)
			}
		}()
		Register("", newFakeConstructor(&fakeDestination{}, nil))
	})

	t.Run("register_nil_constructor_panics", func(t *testing.T) {
		t.Parallel()

		name := uniqueName(t)
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("Register(%q, nil) did not panic", name)
			}
		}()
		Register(name, nil)
	})

	t.Run("names_lists_registered_destinations_sorted", func(t *testing.T) {
		t.Parallel()

		a := uniqueName(t) + "-a"
		b := uniqueName(t) + "-b"
		Register(a, newFakeConstructor(&fakeDestination{name: a}, nil))
		Register(b, newFakeConstructor(&fakeDestination{name: b}, nil))

		names := Names()
		if !slices.Contains(names, a) || !slices.Contains(names, b) {
			t.Fatalf("Names() = %v, want to contain %q and %q", names, a, b)
		}
		if !slices.IsSorted(names) {
			t.Fatalf("Names() = %v, want sorted", names)
		}
	})
}

func TestFakeDestinationDeliverRecordsCall(t *testing.T) {
	t.Parallel()

	fake := &fakeDestination{name: "fake-record"}
	doc := Document{
		ID:          "scan-1",
		Filename:    "2026-08-13T14-32-01_receipt.pdf",
		ContentType: "application/pdf",
		PageCount:   2,
		DocType:     "eingangsrechnung",
	}
	meta := Metadata{Title: "Rechnung", TagIDs: []int{3, 7}}
	cfg := ProfileDestinationConfig{Target: "fake-record", StorageFirst: false}

	if err := fake.Deliver(context.Background(), doc, meta, cfg); err != nil {
		t.Fatalf("Deliver() error = %v, want nil", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.calls))
	}
	got := fake.calls[0]
	if got.doc.ID != doc.ID || got.doc.Filename != doc.Filename {
		t.Fatalf("recorded doc = %+v, want %+v", got.doc, doc)
	}
	if !slices.Equal(got.meta.TagIDs, meta.TagIDs) {
		t.Fatalf("recorded meta.TagIDs = %v, want %v", got.meta.TagIDs, meta.TagIDs)
	}
	if got.cfg.Target != cfg.Target {
		t.Fatalf("recorded cfg.Target = %q, want %q", got.cfg.Target, cfg.Target)
	}
}

func TestFakeDestinationDeliverPropagatesError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("delivery failed")
	fake := &fakeDestination{name: "fake-error", deliverErr: wantErr}

	err := fake.Deliver(context.Background(), Document{}, Metadata{}, ProfileDestinationConfig{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Deliver() error = %v, want %v", err, wantErr)
	}
}
