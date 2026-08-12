#!/usr/bin/env bash
# Test fixture standing in for scanimage(1). This is NOT a real SANE
# binary — it exists only so exec_scanner_test.go can point
# ExecScanner.BinPath at it and exercise the real exec.CommandContext
# + stderr-classification path without any scanner hardware.
#
# The scan scenario is selected via the -d (device) argument that
# buildArgv emits, so each parallel subtest can pick its own scenario
# just by constructing a distinct scanner.Params{Device: ...} — there
# is no shared environment-variable state, which keeps this safe
# under t.Parallel().
set -euo pipefail

if [[ "${1:-}" == "-L" ]]; then
  echo "device \`avision:libusb:001:002' is a KODAK ScanMate i1120 scanner"
  echo "device \`genesys:libusb:001:003' is a Canon LiDE 220 flatbed scanner"
  exit 0
fi

device=""
batch_template=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -d)
      device="$2"
      shift 2
      ;;
    --batch=*)
      batch_template="${1#--batch=}"
      shift
      ;;
    *)
      shift
      ;;
  esac
done

case "$device" in
  fixture:device-error)
    echo "scanimage: sane_start: Error during device I/O" >&2
    exit 1
    ;;
  fixture:no-documents)
    echo "scanimage: sane_start: Document feeder out of documents" >&2
    exit 1
    ;;
  fixture:timeout)
    # exec replaces this process's image with sleep, so a SIGKILL sent
    # to the child by exec.CommandContext's ctx-cancellation kills the
    # sleep directly instead of leaving it as an orphaned grandchild.
    exec sleep 5
    ;;
  *)
    path0=$(printf '%s' "$batch_template" | sed 's/%d/0/')
    path1=$(printf '%s' "$batch_template" | sed 's/%d/1/')
    printf 'PAGE-0-BYTES' > "$path0"
    printf 'PAGE-1-BYTES' > "$path1"
    ;;
esac
