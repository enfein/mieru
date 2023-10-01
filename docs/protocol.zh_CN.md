# mieru 代理协议

为了满足不同场景的需要，mieru 提供了 TCP 和 UDP 两种不同的代理协议。由于 UDP 协议需要尝试更多次数的解密，TCP 协议比 UDP 协议更快。在大多数情况下，我们推荐使用 TCP 协议。

下面是 mieru 代理协议的具体讲解。如果没有特殊说明，所有数据以 big endian 的方式存储。

## 密钥生成方法

TCP 和 UDP 协议共用同一套密钥生成方法。

每一个 mieru 用户都需要提供用户名 `username` 和密码 `password`。从用户名和密码生成加密和解密使用的密钥，需要经历如下几步。

第一步，生成一个哈希密码 `hashedPassword`，其值等于 `password` 附加一个 `0x00` 字节再附加 `username` 得到的字符串的 SHA-256 校验码。

第二步，获取系统当前的时间 `unixTime`，其值等于 1970 年 1 月 1 日到现在经历的秒数。将 `unixTime` 的时刻四舍五入到最接近的 1 分钟，以 uint64 存储为一个 8 字节的字符串，取得该字符串的 SHA-256 校验码，记为 `timeSalt`。

第三步，使用 [pbkdf2](https://en.wikipedia.org/wiki/PBKDF2) 算法生成密钥。其中，使用 `hashedPassword` 作为密码，使用 `timeSalt` 作为盐，迭代次数为 4096，密钥长度为 32 字节，哈希算法为 SHA-256。

由于密钥依赖于系统时间，客户端和服务器之间的时间差不能超过两分钟。服务器可能需要尝试几组不同的时刻才能顺利解密。

mieru 协议允许使用任何 [AEAD](https://en.wikipedia.org/wiki/Authenticated_encryption) 算法进行加密。当前 mieru 版本只实现了 AES-256-GCM 算法。

## 数据段的格式

mieru 收到用户的网络访问请求后，会将原始数据流量切分成小段（fragment），经过加密封装发送到互联网上。每个数据段（segment）中的数据项（field）及其长度如下表所示。

| padding 0 | nonce | encrypted metadata | auth tag of encrypted metadata | padding 1 | encrypted payload | auth tag of encrypted payload | padding 2 |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| ? | 0 or 12 | 32 | 16 | ? | size of original fragment | 16 | ? |

这其中，`encrypted metadata` 和 `auth tag of encrypted metadata` 会出现在每一个数据段中，其它的数据项则不是必须的。`padding 0`, `padding 1` 和 `padding 2` 是随机生成的非加密内容，mieru 使用这些填充数据调节数据段的信息熵，以及连续可打印字符的长度等信息。

### TCP 数据段的规则

使用 TCP 协议时，nonce 在 TCP 连接的每个方向（客户端到服务器、服务器到客户端）只会在第一个数据段出现一次。每传输一个数据段，会进行一次或者两次加密操作，得到加密的元数据，以及（如果有）加密的原始数据载荷。每进行一次加密，nonce 的值会增加 1，变更后的 nonce 将会参与下一组加密的计算。

把原始数据切分成小段时，单个小段的最大长度是 32768 字节。

### UDP 数据段的规则

使用 UDP 协议时，每一个数据段都会包含 nonce，用来解密当前的数据段。

加密后的数据段必须能装入单个 UDP 数据包中。数据段在传输时的大小，不能超过当前网络的 MTU 值。网络的 MTU 值会决定把原始数据切分成小段时，单个小段的最大长度。

## 元数据的格式

每个数据段必定包含一个元数据。元数据的长度固定为 32 字节。当前 mieru 版本定义的元数据类型包含下面两种。

### 会话元数据

会话元数据（session metadata）中的数据项及其长度如下表所示。

| protocol type | unused | timestamp | session ID | sequence number | status code | payload length | suffix length | unused |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| 1 | 1 | 4 | 4 | 4 | 1 | 2 | 1 | 14 |

会话元数据用于下面四种 `protocol type`:

- `openSessionRequest` = 2
- `openSessionResponse` = 3
- `closeSessionRequest` = 4
- `closeSessionResponse` = 5

`timestamp` 的值设定为 1970 年 1 月 1 日到现在经历的分钟数。

如果一个数据段采用了会话元数据，该数据段可以用来传输最多 1024 字节的原始数据载荷。这个载荷的长度记录在 `payload length` 中。

`suffix length` 决定了 `padding 2` 的长度。

### 数据元数据

数据元数据（data metadata）中的数据项及其长度如下表所示。

| protocol type | unused | timestamp | session ID | sequence number | unack sequence number | window size | fragment number | prefix length | payload length | suffix length | unused |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| 1 | 1 | 4 | 4 | 4 | 4 | 2 | 1 | 1 | 2 | 1 | 7 |

数据元数据用于下面四种 `protocol type`:

- `dataClientToServer` = 6
- `dataServerToClient` = 7
- `ackClientToServer` = 8
- `ackServerToClient` = 9

`timestamp`, `session ID` 和 `sequence number` 的定义和用法，与会话元数据相同。

`sequence number`, `unack sequence number` 以及 `window size` 用于流量控制。

`prefix length` 决定了 `padding 1` 的长度，而 `suffix length` 决定了 `padding 2` 的长度。

## UDP Associate 的封装

mieru 支持使用 TCP 和 UDP 代理协议传输 socks5 UDP associate 请求。为了保留 socks5 UDP 数据包的边界，mieru 会对原始 UDP associate 数据包进行如下的封装：

| marker 1 | data length | data | marker 2 |
| :----: | :----: | :----: | :----: |
| 1 | 2 | X | 1 |

其中 `marker 1` 的值恒定为 `0x00`，`data length` 的值为 `X`，`marker 2` 的值恒定为 `0xff`。封装后的结果将作为原始数据交给 TCP 和 UDP 代理协议进行加密和传输。
