# Noise Protocol integration test

This directory contains the container image used by `make
run-container-test-noise` to validate the Noise Protocol Framework
implementation end-to-end on a clean toolchain.

## What it does

The Dockerfile:

1. Starts from the official `golang:1.22` image (the same minor version
   used by the other mieru container tests).
2. Copies the full source tree into `/src`.
3. Pre-downloads modules so the test run itself is hermetic.
4. Runs `go test -race` on `./pkg/cipher` and `./pkg/cipher/noise`,
   which together cover:
   - every supported handshake pattern (NN, NK, NX, XN, XK, XX, IN,
     IK, IX and psk-modified XXpsk3);
   - every supported AEAD cipher (ChaCha20-Poly1305, AES-256-GCM);
   - every supported hash (SHA256, SHA512, BLAKE2s, BLAKE2b);
   - proto decoding, public-key derivation, mismatched prologue /
     PSK failure paths;
   - the `cipher.NewNoiseBlockCipher` adapter (encrypt / decrypt
     round-trips, rejected clone, implicit-nonce guard).

No external processes are involved — the handshake runs over an
in-memory `net.Pipe()` so the test can complete offline inside any
container runtime.

## Running via OrbStack

```sh
# From the repo root:
docker build -t mieru_noise:dev -f test/deploy/noise/Dockerfile .
docker run --rm mieru_noise:dev
```

Or via the Makefile target:

```sh
make run-container-test-noise
```

## Adding a new pattern

When a new pattern is added to `pkg/cipher/noise/config.go`, add a
`TestHandshake_<Pattern>` case to `pkg/cipher/noise/handshake_test.go`
and teach `needsLocalStatic` / `needsRemoteStatic` in the test helpers
about it. The integration test picks up the new case automatically on
the next image rebuild.
