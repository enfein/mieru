# Noise binary-level end-to-end test

This directory contains a containerised end-to-end test that runs
**real** ``mita`` (server) + ``mieru`` (client) binaries wired to use
the Noise Protocol Framework for encryption, and exchanges SOCKS5
traffic through them.

Unlike ``test/deploy/noise/`` which only runs Go unit tests, this
scenario proves the full stack: JSON config → CLI → config validation
→ mux with Noise → TCP Noise handshake → mieru framing on top →
SOCKS5 proxying of a real HTTP echo.

## What it exercises

1. ``mieru keypair noise`` smoke test (the new CLI emits hex keys).
2. ``mita apply config server_tcp.json`` with ``encryption: NOISE``.
3. ``mieru apply config client_tcp.json`` with matching Noise config.
4. ``sockshttpclient -test_case=new_conn -num_request=200`` — fresh
   Noise handshake per connection.
5. ``sockshttpclient -test_case=reuse_conn -num_request=200`` — many
   sessions multiplexed over a single handshake.

## Config snapshot

- Pattern: ``NOISE_XX``
- DH: ``NOISE_DH_25519``
- Cipher: ``NOISE_CIPHER_CHACHA20POLY1305``
- Hash: ``NOISE_HASH_SHA256``
- Keys: hard-coded in the JSON so the test is deterministic; keys are
  test-only and not used by any other scenario.

## Running via OrbStack

```sh
# From the repo root:
make test-binary                # builds bin/mieru, bin/mita, helpers
docker build -t mieru_noise_e2e:dev -f test/deploy/noise_e2e/Dockerfile .
docker run --rm mieru_noise_e2e:dev
```

Or via the Makefile target:

```sh
make run-container-test-noise-e2e
```

## Extending

To add a second pattern (e.g. ``NOISE_IK``) add another pair of
configs + test script alongside ``test_tcp.sh`` and a new ``RUN``
stanza in the Dockerfile. Every test script should source
``libtest.sh`` and follow the apply / start / verify / stop flow used
by the existing one so log handling stays consistent.
