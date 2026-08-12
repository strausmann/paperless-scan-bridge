package scanner

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// fixturePath returns the path to the shell script standing in for
// scanimage(1), relative to this package's test working directory.
func fixturePath() string {
	return "testdata/fixture-scanimage.sh"
}

func TestExecScanner_HappyPath_StreamsFakeBytes(t *testing.T) {
	t.Parallel()

	sc := &ExecScanner{BinPath: fixturePath()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pages, err := sc.Scan(ctx, Params{
		Device:     "fixture:happy",
		Source:     "ADF Duplex",
		Resolution: 300,
		Mode:       "Color",
		Format:     "tiff",
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("len(pages) = %d, want 2", len(pages))
	}

	want := []string{"PAGE-0-BYTES", "PAGE-1-BYTES"}
	for i, p := range pages {
		if p.Index != i {
			t.Errorf("pages[%d].Index = %d, want %d", i, p.Index, i)
		}
		got, err := io.ReadAll(p.Data)
		if err != nil {
			t.Fatalf("read page %d: %v", i, err)
		}
		if string(got) != want[i] {
			t.Errorf("page %d bytes = %q, want %q", i, got, want[i])
		}
		if err := p.Data.Close(); err != nil {
			t.Errorf("close page %d: %v", i, err)
		}
	}
}

func TestExecScanner_HappyPath_ScratchCleanedUpAfterAllPagesClosed(t *testing.T) {
	t.Parallel()

	sc := &ExecScanner{BinPath: fixturePath()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pages, err := sc.Scan(ctx, Params{
		Device: "fixture:happy",
		Source: "ADF Duplex", Resolution: 300, Mode: "Color", Format: "tiff",
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("len(pages) = %d, want 2", len(pages))
	}

	prc, ok := pages[0].Data.(*pageReadCloser)
	if !ok {
		t.Fatalf("pages[0].Data is %T, want *pageReadCloser", pages[0].Data)
	}
	scratchDir := prc.cleanup.dir

	if _, err := os.Stat(scratchDir); err != nil {
		t.Fatalf("scratch dir missing before pages closed: %v", err)
	}

	pages[0].Data.Close()
	if _, err := os.Stat(scratchDir); err != nil {
		t.Fatalf("scratch dir removed too early after closing only 1 of 2 pages: %v", err)
	}

	pages[1].Data.Close()
	if _, err := os.Stat(scratchDir); !os.IsNotExist(err) {
		t.Fatalf("scratch dir still present after all pages closed: err = %v", err)
	}
}

func TestExecScanner_NonZeroExit_ReturnsDeviceError(t *testing.T) {
	t.Parallel()

	sc := &ExecScanner{BinPath: fixturePath()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := sc.Scan(ctx, Params{
		Device: "fixture:device-error",
		Source: "ADF Duplex", Resolution: 300, Mode: "Color", Format: "tiff",
	})
	if !errors.Is(err, ErrDeviceError) {
		t.Fatalf("err = %v, want errors.Is(err, ErrDeviceError)", err)
	}
}

func TestExecScanner_NoDocuments(t *testing.T) {
	t.Parallel()

	sc := &ExecScanner{BinPath: fixturePath()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := sc.Scan(ctx, Params{
		Device: "fixture:no-documents",
		Source: "ADF Duplex", Resolution: 300, Mode: "Color", Format: "tiff",
	})
	if !errors.Is(err, ErrNoDocuments) {
		t.Fatalf("err = %v, want errors.Is(err, ErrNoDocuments)", err)
	}
}

func TestExecScanner_ContextTimeout(t *testing.T) {
	t.Parallel()

	sc := &ExecScanner{BinPath: fixturePath()}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := sc.Scan(ctx, Params{
		Device: "fixture:timeout",
		Source: "ADF Duplex", Resolution: 300, Mode: "Color", Format: "tiff",
	})
	elapsed := time.Since(start)

	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want errors.Is(err, ErrTimeout)", err)
	}
	// The fixture sleeps 5s; a correct implementation must return
	// once ctx expires (~200ms), not wait for the full sleep.
	if elapsed > 4*time.Second {
		t.Fatalf("Scan took %s, want well under the fixture's 5s sleep", elapsed)
	}
}

func TestExecScanner_ListDevices(t *testing.T) {
	t.Parallel()

	sc := &ExecScanner{BinPath: fixturePath()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	devices, err := sc.ListDevices(ctx)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	want := []string{"avision:libusb:001:002", "genesys:libusb:001:003"}
	if len(devices) != len(want) {
		t.Fatalf("devices = %v, want %v", devices, want)
	}
	for i := range want {
		if devices[i] != want[i] {
			t.Errorf("devices[%d] = %q, want %q", i, devices[i], want[i])
		}
	}
}
