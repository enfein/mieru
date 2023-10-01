# mieru Proxy Protocol

To meet the needs of different scenarios, mieru provides two different protocols: TCP and UDP. Because UDP protocol requires more decryption attempts, TCP protocol is faster. In most cases, we recommend using TCP protocol.

The following is a detailed explanation of the mieru proxy protocol. Unless otherwise specified, all data is stored in big endian format.

## Key Generation Method

TCP and UDP protocols share the same key generation method.

Each mieru user needs to provide a username `username` and a password `password`. To generate the key for encryption and decryption from the username and password, the following steps are executed.

The first step is to generate a hashed password `hashedPassword`, whose value is equal to `password` appended with a `0x00` byte and appended with `username`, then takes the SHA-256 checksum.

The second step is to get the current time of the system `unixTime`, whose value is equal to the number of seconds elapsed between January 1, 1970 and now. Round the time of `unixTime` to the nearest 1 minute, and store it as an 8-byte string from uint64. Get the SHA-256 checksum of the string as `timeSalt`.

In the third step, the key is generated using the [pbkdf2](https://en.wikipedia.org/wiki/PBKDF2) algorithm. In this case, `hashedPassword` is used as the password, `timeSalt` is used as the salt, the number of iterations is 4096, the length of the key is 32 bytes, and the hash algorithm is SHA-256.

Since the key depends on the system time, the time difference between the client and the server must not be larger than 2 minutes. The server may need to try several different `timeSalt` to decrypt it successfully.

The mieru protocol allows the use of any [AEAD](https://en.wikipedia.org/wiki/Authenticated_encryption) algorithm for encryption. The current version of mieru only implements the AES-256-GCM algorithm.

## Segment Format

When mieru receives a network access request from a user, it divides the original data stream into small fragments and sends them to the Internet after encryption and encapsulation. The fields and their lengths in each segment are as shown in the following table:

| padding 0 | nonce | encrypted metadata | auth tag of encrypted metadata | padding 1 | encrypted payload | auth tag of encrypted payload | padding 2 |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| ? | 0 or 12 | 32 | 16 | ? | size of original fragment | 16 | ? |

Among these, `encrypted metadata` and `auth tag of encrypted metadata` will appear in every segment, while the other fields are not mandatory. `padding 0`, `padding 1`, and `padding 2` are randomly generated non-encrypted content used by mieru to adjust the information entropy of the segment, and the length of consecutive printable characters, among other factors.

### TCP Segment Rules

When using TCP protocol, the nonce will only appear once in the first segment of each direction of the TCP connection (from the client to the server and from the server to the client). For each segment transmitted, one or two encryption operations will be performed to obtain encrypted metadata and, if present, encrypted raw data payload. With each encryption operation, the nonce value will increase by 1, and the modified nonce will be used in the calculation for the next set of encryption.

When splitting the original data into fragments, the maximum length for an individual fragment is 32768 bytes.

### UDP Segment Rules

When using UDP protocol, each segment will include a nonce used to decrypt the current segment.

The encrypted segment must fit into a single UDP packet. The size of the segment during transmission cannot exceed the Maximum Transmission Unit (MTU) value of the current network. The network's MTU value determines the maximum length for an individual fragment when splitting the original data into fragments.

## Metadata Format

Each segment must contain metadata, and the length of the metadata is fixed at 32 bytes. The current version of mieru defines two types of metadata as follows.

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

## UDP Associate Encapsulation

mieru supports transmission of socks5 UDP associate requests using TCP and UDP proxy protocols. In order to preserve the boundaries of socks5 UDP packets, mieru encapsulates the raw UDP associate packets as follows:

| marker 1 | data length | data | marker 2 |
| :----: | :----: | :----: | :----: |
| 1 | 2 | X | 1 |

The value of `marker 1` is constant `0x00`, the value of `data length` is `X`, and the value of `marker 2` is constant `0xff`. The encapsulated result is passed to TCP and UDP proxy protocols as the raw data for encryption and transmission.
