# mieru Proxy Protocol

To meet the needs of different scenarios, mieru provides two different protocols: TCP and UDP. Because UDP protocol requires more decryption attempts, TCP protocol is faster. In most cases, we recommend using TCP protocol.

The following is a detailed explanation of the mieru proxy protocol. Unless otherwise specified, all data is stored in big endian format.

## Key Generation Method

TCP and UDP protocols share the same key generation method.

Each mieru user needs to provide a username `username` and a password `password`. To generate the key for encryption and decryption from the username and password, the following steps are executed.

The first step is to generate a hashed password `hashedPassword`, whose value is equal to `password` appended with a `0x00` byte and appended with `username`, then takes the SHA-256 checksum.

The second step is to get the current time of the system `unixTime`, whose value is equal to the number of seconds elapsed between January 1, 1970 and now. Round the time of `unixTime` to the nearest 2 minutes, and store it as an 8-byte string from uint64. Get the SHA-256 checksum of the string as `timeSalt`.

In the third step, the key is generated using the [pbkdf2](https://en.wikipedia.org/wiki/PBKDF2) algorithm. In this case, `hashedPassword` is used as the password, `timeSalt` is used as the salt, the number of iterations is 64, the length of the key is 32 bytes, and the hash algorithm is SHA-256.

Since the key depends on the system time, the time difference between the client and the server must not be larger than 4 minutes. The server needs to try maximum 3 different `timeSalt` to decrypt it successfully.

The mieru protocol allows the use of any [AEAD](https://en.wikipedia.org/wiki/Authenticated_encryption) algorithm for encryption. The nonce length of the AEAD algorithm must be 24 bytes. The current version of mieru only implements the XChaCha20-Poly1305 algorithm.

To accelerate user lookup, the last 4 bytes of the nonce is replace by the first 4 bytes of a SHA-256 output. The input of SHA-256 is user name concatenate by the first 16 bytes of the nonce.

## Segment Format

When mieru receives a network access request from a user, it divides the original data stream into small fragments and sends them to the Internet after encryption and encapsulation. The fields and their lengths in each segment are as shown in the following table:

| padding 0 | nonce | encrypted metadata | auth tag of encrypted metadata | padding 1 | encrypted or low entropy encoded payload body | auth tag of encrypted payload | padding 2 |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| ? | 0 or 24 | 32 | 16 | ? | original or encoded body size | 16 | ? |

Among these, `encrypted metadata` and `auth tag of encrypted metadata` will appear in every segment, while the other fields are not mandatory. `padding 0`, `padding 1`, and `padding 2` are randomly generated non-encrypted content used by mieru to adjust the information entropy of the segment, and the length of consecutive printable characters, among other factors.

### TCP Segment Rules

When using TCP protocol, the nonce will only appear once in the first segment of each direction of the TCP connection (from the client to the server and from the server to the client). For each segment transmitted, one or two encryption operations will be performed to obtain encrypted metadata and, if present, encrypted raw data payload. With each encryption operation, the nonce value will increase by 1, and the modified nonce will be used in the calculation for the next set of encryption.

When splitting the original data into fragments, the maximum length for an individual fragment is 32768 bytes. The exception is low entropy mode `LOW_ENTROPY_MODE_32`, whose maximum is 32764 bytes so its encoded length fits the 16-bit `payload length` field. A 32768-byte application write in that mode is split across multiple segments.

### UDP Segment Rules

When using UDP protocol, each segment will include a nonce used to decrypt the current segment.

The encrypted segment must fit into a single UDP packet. The size of the segment during transmission cannot exceed the Maximum Transmission Unit (MTU) value of the current network. The network's MTU value determines the maximum length for an individual fragment when splitting the original data into fragments.

## Metadata Format

Each segment must contain metadata, and the length of the metadata is fixed at 32 bytes. The current version of mieru defines three types of metadata as follows.

### Session Metadata

The fields and their lengths in the session metadata are as shown in the following table:

| protocol type | unused | timestamp | session ID | sequence number | status code | payload length | suffix length | unused |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| 1 | 1 | 4 | 4 | 4 | 1 | 2 | 1 | 14 |

The session metadata is used for the following four `protocol type`:

- `openSessionRequest` = 2
- `openSessionResponse` = 3
- `closeSessionRequest` = 4
- `closeSessionResponse` = 5

The value of timestamp is set to the number of minutes elapsed since January 1, 1970.

If a segment selects session metadata, the segment can be used to transmit a maximum of 1024 bytes of raw payload data. The length of this payload is recorded in `payload length`.

The `suffix length` determines the length of `padding 2`.

### Data Metadata

The fields and their lengths in the data metadata are as shown in the following table:

| protocol type | unused | timestamp | session ID | sequence number | unack sequence number | window size | fragment number | prefix length | payload length | suffix length | unused |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| 1 | 1 | 4 | 4 | 4 | 4 | 2 | 1 | 1 | 2 | 1 | 7 |

The data metadata is used for the following four `protocol type`:

- `dataClientToServer` = 6
- `dataServerToClient` = 7
- `ackClientToServer` = 8
- `ackServerToClient` = 9

The definitions and usage of `timestamp`, `session ID`, and `sequence number` are the same as in session metadata.

`sequence number`, `unack sequence number`, and `window size` are used for flow control.

`prefix length` determines the length of `padding 1`, while `suffix length` determines the length of `padding 2`.

### Data Metadata (Low Entropy Extension)

The low entropy extension uses the same structure as data metadata, with the following fields replacing the previously unused bytes:

| protocol type | low entropy mode | timestamp | session ID | sequence number | unack sequence number | window size | fragment number | prefix length | payload length | suffix length | low entropy mask | extracted payload length | low entropy mask rotation |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| 1 | 1 | 4 | 4 | 4 | 4 | 2 | 1 | 1 | 2 | 1 | 4 | 2 | 1 |

This extension is used for the following two `protocol type` values:

- `dataClientToServerLowEntropy` = 10
- `dataServerToClientLowEntropy` = 11

`low entropy mode` is 1, 2, 3, or 4 when each 64-bit low entropy chunk contains 32, 40, 48, or 56 payload bits, respectively; the remaining bits are padding. A value of 0 disables low entropy encoding.

The 4-byte `low entropy mask` is repeated once to form the 8-byte mask, where each 1-bit identifies a payload position in each chunk, and each 0-bit identifies a padding position. `payload length` includes the low entropy padding, while `extracted payload length` records the payload size after the padding is removed.

`low entropy mask rotation` controls how the 8-byte mask rotates between adjacent 64-bit chunks. A value from 1 through 15 (lower 4 bits) rotates the mask right by that many bits; a value equal to 16 times a number from 1 through 15 (higher 4 bits) rotates it left by that number of bits. A value of 0 keeps the mask unchanged.

#### Low Entropy Payload Encoding

Low entropy encoding is used only by protocol types 10 and 11. The sender first applies the normal payload AEAD encryption, then splits the result into a ciphertext body and its 16-byte authentication tag. Only the ciphertext body is encoded. The unchanged tag follows the encoded body immediately and is not included in either `payload length` or the low entropy transform. `extracted payload length` is the ciphertext body length before expansion; because AEAD encryption preserves the plaintext length, it is also the application fragment length.

The mode determines the source capacity `C` of each 8-byte encoded chunk:

| mode | configuration value | source bytes `C` | 1-bits in 32-bit half-mask | full-chunk expansion |
| :----: | :---- | :----: | :----: | :----: |
| 1 | `LOW_ENTROPY_MODE_32` | 4 | 16 | 2.0x |
| 2 | `LOW_ENTROPY_MODE_40` | 5 | 20 | 1.6x |
| 3 | `LOW_ENTROPY_MODE_48` | 6 | 24 | approximately 1.34x |
| 4 | `LOW_ENTROPY_MODE_56` | 7 | 28 | approximately 1.15x |

For an extracted body of `N` bytes, `payload length` is `ceil(N / C) * 8`. A final partial source chunk still occupies 8 bytes.

For each transmission, the sender generates the 32-bit half-mask with the number of 1-bits required by the mode and repeats it to form the initial 64-bit mask. Chunk 0 uses this mask unchanged. Chunk `i` uses the initial mask rotated by `i * R` bits in the direction encoded by `low entropy mask rotation`; rotation is always calculated from the initial mask.

Each source chunk is interpreted in big-endian byte order and placed in the low-order source bits. The sender deposits those bits into the 1-bit positions of the current mask. Every other position is filled with one uniform padding bit. That bit may be 0 or 1 and is stable for a given sender host and mieru version. In the final partial chunk, unused positions selected by the mask are padding too.

The receiver infers the padding value from the first chunk and requires every non-data position in every chunk to have that value. Mixed padding, an invalid mode or rotation, the wrong mask population, or inconsistent encoded and extracted lengths makes the segment invalid. Changes to selected ciphertext bits or to the unchanged authentication tag are rejected by AEAD authentication.

All multi-byte metadata fields and encoded 64-bit chunks use big-endian wire order. Here is a full example of mode `LOW_ENTROPY_MODE_32`:

```text
half-mask:               0x0f0f0f0f
64-bit mask:             0x0f0f0f0f0f0f0f0f
rotation:                0
source:                  12 34 56 78
encoded, padding bit 0:  01 02 03 04 05 06 07 08
encoded, padding bit 1:  f1 f2 f3 f4 f5 f6 f7 f8
```

## UDP Associate Encapsulation

mieru supports transmission of socks5 UDP associate requests using TCP and UDP proxy protocols. In order to preserve the boundaries of socks5 UDP packets, mieru encapsulates the raw UDP associate packets as follows:

| marker 1 | data length | data | marker 2 |
| :----: | :----: | :----: | :----: |
| 1 | 2 | X | 1 |

The value of `marker 1` is constant `0x00`, the value of `data length` is `X`, and the value of `marker 2` is constant `0xff`. The encapsulated result is passed to TCP and UDP proxy protocols as the raw data for encryption and transmission.
