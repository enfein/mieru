# mieru Proxy Protocol

In order to meet the needs of different scenarios, mieru provides two different proxy protocols, TCP and UDP. TCP protocol might be faster than UDP protocol. However, TCP protocol is more likely to be sniffed than UDP protocol.

The following is an explanation of the mieru proxy protocol.

## Key generation method

TCP and UDP protocols share the same key generation method.

Each mieru user needs to provide a username `username` and a password `password`. To generate the key for encryption and decryption from the username and password, the following steps are required.

The first step is to generate a hashed password `hashedPassword`, whose value is equal to `password` appended with a `0x00` byte and appended with `username`, then takes the SHA-256 checksum.

The second step is to get the current time of the system `unixTime`, whose value is equal to the number of seconds elapsed between January 1, 1970 and now. Round the time of `unixTime` to the nearest 5 minutes, and store it as an 8-byte string in little endian uint64, to get the SHA-256 checksum of the string as `timeSalt`.

In the third step, the key is generated using the [pbkdf2](https://en.wikipedia.org/wiki/PBKDF2) algorithm. In this case, `hashedPassword` is used as the password, `timeSalt` is used as the salt, the number of iterations is 4096, the length of the key is 32 bytes, and the hash algorithm is SHA-256.

Since the key depends on the system time, the time difference between the client and the server must not be too large. The server may need to try several different `timeSalt` to decrypt it successfully.

## TCP based proxy protocol

TCP is a streaming transport protocol and applications don't know the start and end points of individual TCP packets. To facilitate encryption and decryption operations, mieru introduces a data segment size identifier into the byte stream. The size of the **data segment** is independent of the size of the packet transmitted in the network.

The mieru TCP proxy protocol performs two layers of encapsulation on top of the original data traffic. In the first layer, random data padding is added, changing the length of the data. For an explanation to this part, please read the section [Format of decrypted data segments]. In the second layer, the packets are encrypted using the [AEAD](https://en.wikipedia.org/wiki/Authenticated_encryption) algorithm. Please read the section [Format of encrypted data segments] for information about this.

### Format of decrypted data segments

We denote the raw socks5 data traffic as `data`, which is `X` bytes long. When creating the first layer of wrappers, mieru may padding random data at the end of the original data. We denote the random data as `padding` and its length is `Y` bytes. The contents and length of the various parts of the decrypted packet are shown below.

| data length | data + padding length | data | padding |
| :----: | :----: | :----: | :----: |
| 2 | 2 | X | Y |

The value of `data length` is `X` and the value of `data + padding length` is `X + Y`, both encoded in little endian. mieru TCP protocol specifies that the value of `X + Y` cannot be larger than 16380. it is easy to see that the first four bytes of the decrypted data segment tell the size of the current data segment.

### Format of encrypted data segments

We denote `decrypted` for the data obtained after the first layer, and its length is `X + Y + 4`, and we denote the value of this length as `decrypted length`. This length value can be stored in two bytes. `decrypted` is encrypted to `encrypted`, and its length is also `X + Y + 4`. `decrypted length` is encrypted to `encrypted length`, which is also two bytes long.

The contents and length of the encrypted data segment are shown below.

| nonce | encrypted length | authentication tag of encrypted length | encrypted | authentication tag of encrypted |
| :----: | :----: | :----: | :----: | :----: |
| 0 or 12 | 2 | 16 | X + Y + 4 | 16 |

The nonce appears only once in the first data segment in each direction of the TCP connection (client to server, server to client). Each encrypted data segment transmits two sets of encrypted information: the first set transmits two bytes of `encrypted length`, and the second set transmits `encrypted` data of `X + Y + 4` bytes. AEAD algorithm adds 16 bytes of authentication tag after each set of encrypted information. The value of nonce is increased by 1 for each set of encrypted messages, and the changed nonce will be used in the next set of encryption or decryption calculations.

There is no restriction on the AEAD algorithms, as long as the client and server use the same algorithm. Some of the popular algorithms are AES-128-GEM, AES-256-GCM, ChaCha20-Poly1305, etc. In this implementation, we use the AES-256-GCM algorithm.

The encrypted data segment will be split by the operating system into appropriately sized TCP packets, and sent to the Internet.

## UDP based proxy protocol

mieru UDP proxy protocol also has two layers of encapsulation on top of the original data traffic. The first layer uses a modified [KCP](https://github.com/skywind3000/kcp) protocol that implements random data padding and automatic retransmission ([ARQ](https://en.wikipedia.org/wiki/Automatic_repeat_request)). For an explanation to this part, please read the section [Format of decrypted data segments] and [Keep alive]. The second layer uses the AEAD algorithm to encrypt the packets before sending them to the Internet. Please read the section [Format of encrypted data segments] for information on this.

### Format of decrypted data segments

We denote the raw socks5 data traffic as `data`, which is `X` bytes long. When creating the first layer of wrappers, mieru may padding random data at the end of the original data. We denote the random data as `padding` and its length is `Y` bytes. The contents and length of the various parts of the decrypted packet are shown below.

| KCP header | data | padding |
| :----: | :----: | :----: |
| 24 | X | Y |

The contents and length of the `KCP header` are as follows.

| conversation ID | command | fragment count | receive window size | timestamp | sequence number | unacknowledged sequence number | data length | data + padding length |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| 4 | 1 | 1 | 2 | 4 | 4 | 4 | 2 | 2 |

KCP uses little endian to encode multi-byte content. There are many references about the KCP protocol, so I will not go into details here. The following section explains the differences between mieru's modified KCP and the original protocol.

First, mieru uses a different set of `command` values. In mieru, the individual `command` and their corresponding meanings are as follows.

| command | action |
| :----: | :----: |
| 0x01 | send data |
| 0x02 | acknowledge |
| 0x03 | ask remote window size |
| 0x04 | reply my window size |

Second, the 4-byte `length` at the end of the original KCP protocol is split into a 2-byte `data length` and a 2-byte `data + padding length` in mieru. As defined above, the value of `data length` should be `X` and the value of `data + padding length` should be `X + Y`.

### Keep alive

In order to reclaim memory resources timely, the server periodically clears inactive KCP sessions. If a client wants to maintain a KCP session, it needs to send heartbeat packets to the server periodically. The heartbeat packets should be sent no more than 30 seconds apart.

### Format of encrypted data segments

We denote `decrypted` for the data obtained after the first layer, and its length is `X + Y + 24`. The second layer uses the AEAD algorithm to encrypt `decrypted` to get `encrypted`. The contents and length of the encrypted packet are shown below.

| nonce | encrypted | authentication tag |
| :----: | :----: | :----: |
| 12 | X + Y + 24 | 16 |

There are no restrictions on the AEAD algorithms that can be used for encryption. In this implementation, we use the AES-256-GCM algorithm.

The data after the second layer is sent to the Internet as a **single UDP packet**. Therefore, the size of the data after the second layer of encapsulation should not exceed the MTU value of the current network. mieru UDP protocol expects a maximum load of 1500 bytes for a single Ethernet data frame. In this reference implementation, we use a default maximum load of 1400 bytes.

## UDP associate encapsulation

mieru supports transmission of socks5 UDP associate requests using TCP and UDP proxy protocols. In order to preserve the boundaries of UDP packets, mieru encapsulates the raw UDP associate packets as follows.

| marker 1 | data length | data | marker 2 |
| :----: | :----: | :----: | :----: |
| 1 | 2 | X | 1 |

The value of `marker 1` is constant `0x00`, the value of `data length` is `X`, using little endian encoding, and the value of `marker 2` is constant `0xff`. The encapsulated result is passed to TCP and UDP proxy protocols as the raw data for encryption and transmission.
