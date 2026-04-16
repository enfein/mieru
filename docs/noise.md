# Noise Protocol Framework in mieru

Starting from v3.31.0 mieru can replace its default password-based
XChaCha20-Poly1305 encryption scheme with a full [Noise Protocol
Framework](https://noiseprotocol.org) handshake. Noise gives mieru:

- **Real ephemeral Diffie-Hellman** on every connection, producing
  forward-secret session keys that cannot be recovered even if the
  static password is later leaked.
- **Choice of cipher suite**: the AEAD cipher, hash function and
  handshake pattern are all picked in config. The selection is encoded
  as a canonical Noise name such as `Noise_XX_25519_ChaChaPoly_SHA256`.
- **Mutual authentication** when the chosen pattern includes static
  keys on both sides (XX, IK, IX, ...).

Noise is optional. When `encryption` is left unset the classic mieru
protocol described in [protocol.md](protocol.md) is used. Noise is a
per-profile and per-server setting so a single binary can host both
schemes on different ports.

## Quick start

1. Generate a long-term static keypair on each machine:

    ```sh
    mieru keypair noise
    # prints hex-encoded private key + public key to stdout
    ```

    The server does the same with `mita keypair noise`.

2. Tell the client about the server's public key:

    ```json
    {
        "profiles": [
            {
                "profileName": "default",
                "user": { "name": "baozi", "password": "manlianpenfen" },
                "servers": [
                    {
                        "domainName": "example.com",
                        "portBindings": [
                            { "port": 8964, "protocol": "TCP" }
                        ]
                    }
                ],
                "encryption": "NOISE",
                "noise": {
                    "pattern":               "NOISE_XX",
                    "dh":                    "NOISE_DH_25519",
                    "cipher":                "NOISE_CIPHER_CHACHA20POLY1305",
                    "hash":                  "NOISE_HASH_SHA256",
                    "localStaticPrivateKey": "<client private, hex>",
                    "remoteStaticPublicKey": "<server public, hex>"
                }
            }
        ],
        "activeProfile": "default",
        "rpcPort": 8989,
        "socks5Port": 1080
    }
    ```

3. Tell the server about its own static key:

    ```json
    {
        "portBindings": [{ "port": 8964, "protocol": "TCP" }],
        "users": [ { "name": "baozi", "password": "manlianpenfen" } ],
        "encryption": "NOISE",
        "noise": {
            "pattern":                "NOISE_XX",
            "localStaticPrivateKey":  "<server private, hex>"
        }
    }
    ```

The server does **not** need to know the client's static key ahead of
time when the pattern is XX — it is transmitted during the handshake.
Use IK if you want the server to reject unknown clients up front.

## Configuration reference

`encryption` accepts:

| value                  | meaning                                               |
| ---------------------- | ----------------------------------------------------- |
| `XCHACHA20_POLY1305`   | Default password-based scheme. `noise` is ignored.    |
| `NOISE`                | Noise handshake. `noise` must be present and valid.   |

`noise` fields:

| field                    | type              | required                                          | description |
| ------------------------ | ----------------- | ------------------------------------------------- | ----------- |
| `pattern`                | `NoisePattern`    | recommended                                       | Handshake pattern (see below). Default `NOISE_XX`. |
| `dh`                     | `NoiseDH`         | no                                                | DH function. Default `NOISE_DH_25519`. |
| `cipher`                 | `NoiseCipher`     | no                                                | AEAD cipher. Default `NOISE_CIPHER_CHACHA20POLY1305`. |
| `hash`                   | `NoiseHash`       | no                                                | Hash function. Default `NOISE_HASH_SHA256`. |
| `localStaticPrivateKey`  | hex string        | when the pattern has a local static for this role | 32-byte private key, hex-encoded. |
| `localStaticPublicKey`   | hex string        | no                                                | Derived from `localStaticPrivateKey` when omitted. |
| `remoteStaticPublicKey`  | hex string        | when the initiator must know it up front          | Required by NK, XK, IK, K, N, X for the initiator. |
| `presharedKey`           | hex string        | when a `pskN` modifier is desired                 | 32-byte PSK, hex-encoded. |
| `presharedKeyPlacement`  | int32             | when `presharedKey` is set                        | Token position, e.g. `3` for `XXpsk3`. |
| `prologue`               | hex string        | no                                                | Arbitrary bytes mixed into the handshake hash. |

### Supported handshake patterns

mieru exposes every pattern defined in §7 of the Noise specification:

| name      | authentication | 0-RTT? | notes                                           |
| --------- | -------------- | ------ | ----------------------------------------------- |
| `NOISE_NN`| none           | no     | Testing only. Vulnerable to active MitM.        |
| `NOISE_NK`| server         | yes    | Client knows server static key.                 |
| `NOISE_NX`| server         | no     | Server transmits static key in message 2.       |
| `NOISE_XN`| client         | no     | Client transmits static key in message 3.       |
| `NOISE_XK`| mutual         | no     | Client knows server static key.                 |
| `NOISE_XX`| mutual         | no     | **Recommended default**.                        |
| `NOISE_IN`| client         | no     | Client static sent in message 1 (no privacy).   |
| `NOISE_IK`| mutual         | yes    | Client knows server static key. 1-RTT.          |
| `NOISE_IX`| mutual         | no     | Client transmits static key in message 1.       |
| `NOISE_K` | mutual         | yes    | One-way. Both static keys known a priori.       |
| `NOISE_N` | server         | yes    | One-way. Client has no static key.              |
| `NOISE_X` | mutual         | yes    | One-way. Client transmits static key.           |

Set `presharedKey` + `presharedKeyPlacement` to switch to a `pskN`
variant. For example, `pattern = NOISE_XX`, `presharedKey = <32 bytes>`,
`presharedKeyPlacement = 3` yields `Noise_XXpsk3_25519_ChaChaPoly_SHA256`.

### Supported cipher suite components

| DH          | Cipher                              | Hash           |
| ----------- | ----------------------------------- | -------------- |
| `NOISE_DH_25519` | `NOISE_CIPHER_CHACHA20POLY1305` | `NOISE_HASH_SHA256`  |
|             | `NOISE_CIPHER_AES256GCM`            | `NOISE_HASH_SHA512`  |
|             |                                     | `NOISE_HASH_BLAKE2S` |
|             |                                     | `NOISE_HASH_BLAKE2B` |

Any combination of the rows in each column is valid. `Curve448` and
additional ciphers may be added in the future; open an issue if you
have a use case.

## Wire format

A Noise session has two phases:

1. **Handshake phase.** Each handshake message is framed as a 2-byte
   big-endian length prefix followed by that many bytes of Noise
   handshake payload. The number of messages depends on the pattern
   (1 for N/K/X, 2 for NK/NN/IK/IX/IN, 3 for XK/XN/XX/NX). Every byte
   of a handshake message is output directly from
   [flynn/noise](https://github.com/flynn/noise), which implements the
   reference Noise state machine.

2. **Transport phase.** After both sides compute the shared state, each
   direction of the connection has its own AEAD `CipherState` that
   maintains a 64-bit monotonic nonce. The nonce is **not** transmitted
   — every payload on the wire is just the AEAD output:

    ```
    | encrypted payload | auth tag |
    | -----------------:| --------:|
    |      N bytes      |  16 B    |
    ```

   The tag size is 16 for both ChaCha20-Poly1305 and AES-GCM. Each
   `Encrypt` / `Decrypt` call advances the local counter by one.
   Replays are inherently rejected because the peer's counter will no
   longer match.

mieru's segment format (see [protocol.md](protocol.md)) still wraps
individual fragments. In Noise mode, the password-derived cipher block
is replaced with a noise-derived cipher block that satisfies the same
`cipher.BlockCipher` interface — the rest of the protocol layer is
agnostic.

## Security notes

- **Forward secrecy.** All non-K patterns exchange at least one
  ephemeral keypair, so every session uses fresh keys. Even a complete
  compromise of static keys after the fact does not let an attacker
  decrypt recorded traffic.
- **Active MitM.** Patterns ending in `N` (NN, XN, IN) do not
  authenticate the server. Use them only for testing or behind another
  authenticated channel.
- **Static-key privacy.** In NK, XK, IK and K-family patterns the
  initiator must send the server's public key in cleartext on the
  first message (actually the responder's static is *not* sent — the
  initiator just encrypts towards it). If an observer already knows
  the set of possible server keys, they can correlate sessions. Use a
  psk-modified pattern or NOISE_XX if this matters.
- **Channel binding.** The post-handshake handshake hash is exposed as
  `transport.ChannelBinding` and is equal on both peers. Applications
  that layer additional authentication on top of Noise should bind
  challenges to this value.
- **User hints.** The default mieru scheme derives per-user keys from
  each user's password so that the server can identify the user from
  the nonce prefix alone. Noise does not use passwords; per-user
  identification is instead done via the static public key. To run
  multiple users on a single Noise endpoint, either use IK so that the
  server reads the initiator's static key directly, or use a
  psk-modified pattern where the PSK is derived from the user's
  password (`cipher.HashPassword`).

## Implementation

The Noise logic lives in two small packages:

- [`pkg/cipher/noise`](../pkg/cipher/noise) — wraps
  [`github.com/flynn/noise`](https://github.com/flynn/noise), adds a
  typed `Config`, validation, hex/proto decoding and a framed
  `Handshake.Run` helper.
- [`pkg/cipher/noise_cipher.go`](../pkg/cipher/noise_cipher.go) —
  exposes the handshake's transport `CipherState` as a
  `cipher.BlockCipher`, the same interface used by the default
  XChaCha20-Poly1305 cipher.

The mieru protocol layer does not know which scheme it is talking to;
both cipher kinds implement the same interface.

### Interop testing

`make run-container-test-noise` builds a dedicated OrbStack / Docker
image that runs all Noise unit tests plus a self-contained end-to-end
round trip through `cipher.NewNoiseBlockCipher`. See
[`test/deploy/noise/`](../test/deploy/noise) for the scenario script.

### Current integration status

- Library: complete (17 unit tests, all patterns from §7 of the Noise
  spec are exposed).
- Configuration: complete (`base.proto`, `clientcfg.proto`,
  `servercfg.proto`, plus JSON validation + `appctl` wiring).
- CLI: `mieru keypair noise` / `mita keypair noise` generate a fresh
  Curve25519 static keypair and print both values as hex on stdout.
- Protocol wire-up: **complete for TCP**. The mux runs the Noise
  handshake on every freshly dialed / accepted connection before any
  mieru frame flows. The derived transport `CipherState` pair is
  adapted to two `cipher.BlockCipher` values that are installed
  directly on the stream underlay's send/recv slots, bypassing the
  password-based key derivation. See
  [`pkg/protocol/noise_wire.go`](../pkg/protocol/noise_wire.go) and
  the `usesNoise()` branches in [`pkg/protocol/mux.go`](../pkg/protocol/mux.go).
- UDP: intentionally rejected at connection time with a clear error
  (`"noise encryption is not supported over UDP yet; use a TCP port
  binding or switch encryption to XCHACHA20_POLY1305"`). Adding a
  datagram-aware handshake with per-source-address state tracking is a
  future enhancement.
- End-to-end: [`test/deploy/noise_e2e/`](../test/deploy/noise_e2e)
  builds the real `mieru` + `mita` binaries into a container,
  configures them with Noise_XX JSON, and runs SOCKS5 traffic through
  the Noise tunnel (`make run-container-test-noise-e2e`).
