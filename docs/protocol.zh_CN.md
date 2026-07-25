# mieru 代理协议

为了满足不同场景的需要，mieru 提供了 TCP 和 UDP 两种不同的代理协议。由于 UDP 协议需要尝试更多次数的解密，TCP 协议比 UDP 协议更快。在大多数情况下，我们推荐使用 TCP 协议。

下面是 mieru 代理协议的具体讲解。如果没有特殊说明，所有数据以 big endian 的方式存储。

## 密钥生成方法

TCP 和 UDP 协议共用同一套密钥生成方法。

每一个 mieru 用户都需要提供用户名 `username` 和密码 `password`。从用户名和密码生成加密和解密使用的密钥，需要经历如下几步。

第一步，生成一个哈希密码 `hashedPassword`，其值等于 `password` 附加一个 `0x00` 字节再附加 `username` 得到的字符串的 SHA-256 校验码。

第二步，获取系统当前的时间 `unixTime`，其值等于 1970 年 1 月 1 日到现在经历的秒数。将 `unixTime` 的时刻四舍五入到最接近的 2 分钟，以 uint64 存储为一个 8 字节的字符串，取得该字符串的 SHA-256 校验码，记为 `timeSalt`。

第三步，使用 [pbkdf2](https://en.wikipedia.org/wiki/PBKDF2) 算法生成密钥。其中，使用 `hashedPassword` 作为密码，使用 `timeSalt` 作为盐，迭代次数为 64，密钥长度为 32 字节，哈希算法为 SHA-256。

由于密钥依赖于系统时间，客户端和服务器之间的时间差不能超过 4 分钟。服务器最多需要尝试 3 组不同的时刻才能顺利解密。

mieru 协议允许使用任何 [AEAD](https://en.wikipedia.org/wiki/Authenticated_encryption) 算法进行加密。算法的 nonce 长度必须为 24 字节。当前 mieru 版本只实现了 XChaCha20-Poly1305 算法。

为了加快用户查找，nonce 的最后 4 个字节被替换为 SHA-256 输出的前 4 个字节，其中 SHA-256 的输入是用户名再接上 nonce 的前 16 个字节。

## 数据段的格式

mieru 收到用户的网络访问请求后，会将原始数据流量切分成小段（fragment），经过加密封装发送到互联网上。每个数据段（segment）中的数据项（field）及其长度如下表所示。

| padding 0 | nonce | encrypted metadata | auth tag of encrypted metadata | padding 1 | encrypted or low entropy encoded payload body | auth tag of encrypted payload | padding 2 |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| ? | 0 or 24 | 32 | 16 | ? | original or encoded body size | 16 | ? |

这其中，`encrypted metadata` 和 `auth tag of encrypted metadata` 会出现在每一个数据段中，其它的数据项则不是必须的。`padding 0`, `padding 1` 和 `padding 2` 是随机生成的非加密内容，mieru 使用这些填充数据调节数据段的信息熵，以及连续可打印字符的长度等信息。

### TCP 数据段的规则

使用 TCP 协议时，nonce 在 TCP 连接的每个方向（客户端到服务器、服务器到客户端）只会在第一个数据段出现一次。每传输一个数据段，会进行一次或者两次加密操作，得到加密的元数据，以及（如果有）加密的原始数据载荷。每进行一次加密，nonce 的值会增加 1，变更后的 nonce 将会参与下一组加密的计算。

把原始数据切分成小段时，单个小段的最大长度是 32768 字节。低熵模式 `LOW_ENTROPY_MODE_32` 是例外，其上限为 32764 字节，以确保编码后的长度能装入 16 位的 `payload length` 字段。该模式下，32768 字节的应用写入会被拆分到多个数据段中。

### UDP 数据段的规则

使用 UDP 协议时，每一个数据段都会包含 nonce，用来解密当前的数据段。

加密后的数据段必须能装入单个 UDP 数据包中。数据段在传输时的大小，不能超过当前网络的 MTU 值。网络的 MTU 值会决定把原始数据切分成小段时，单个小段的最大长度。

## 元数据的格式

每个数据段必定包含一个元数据。元数据的长度固定为 32 字节。当前 mieru 版本定义的元数据类型包含下面三种。

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

### 数据元数据（低熵扩展）

低熵扩展采用与数据元数据相同的结构，并使用以下字段替换原先未使用的字节。

| protocol type | low entropy mode | timestamp | session ID | sequence number | unack sequence number | window size | fragment number | prefix length | payload length | suffix length | low entropy mask | extracted payload length | low entropy mask rotation |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| 1 | 1 | 4 | 4 | 4 | 4 | 2 | 1 | 1 | 2 | 1 | 4 | 2 | 1 |

此扩展用于下面两种 `protocol type` 值：

- `dataClientToServerLowEntropy` = 10
- `dataServerToClientLowEntropy` = 11

当每个 64 位低熵数据块分别包含 32、40、48 或 56 位载荷数据时，`low entropy mode` 的值依次为 1、2、3 或 4，其余位为填充。值为 0 时禁用低熵编码。

将 4 字节的 `low entropy mask` 重复一次，组成 8 字节的掩码，其中值为 1 的位用于标识每个数据块中的载荷位置，值为 0 的位用于标识填充位置。`payload length` 包含低熵填充，而 `extracted payload length` 记录移除填充后的载荷长度。

`low entropy mask rotation` 控制相邻 64 位数据块之间 8 字节掩码的旋转方式。值为 1 至 15 时（低 4 位），掩码向右旋转相应的位数；值为 1 至 15 中某个数的 16 倍时（高 4 位），掩码向左旋转该数对应的位数。值为 0 时掩码保持不变。

#### 低熵载荷编码

低熵编码仅用于协议类型 10 和 11。发送端首先进行普通的载荷 AEAD 加密，然后将结果拆分为密文正文和 16 字节认证标签。只有密文正文参与低熵编码。保持不变的认证标签紧跟在编码后的正文之后，既不计入 `payload length`，也不参与低熵变换。`extracted payload length` 是扩展前的密文正文长度；由于 AEAD 加密保持明文长度不变，它也等于应用分片长度。

模式决定每个 8 字节编码块的源数据容量 `C`：

| 模式 | 配置值 | 源数据字节数 `C` | 32 位半掩码中的 1 位数 | 完整块扩展倍数 |
| :----: | :---- | :----: | :----: | :----: |
| 1 | `LOW_ENTROPY_MODE_32` | 4 | 16 | 2.0 倍 |
| 2 | `LOW_ENTROPY_MODE_40` | 5 | 20 | 1.6 倍 |
| 3 | `LOW_ENTROPY_MODE_48` | 6 | 24 | 约 1.34 倍 |
| 4 | `LOW_ENTROPY_MODE_56` | 7 | 28 | 约 1.15 倍 |

对于长度为 `N` 字节的待提取正文，`payload length` 等于 `ceil(N / C) * 8`。最后一个不完整的源数据块仍占用 8 字节。

每次发送时，发送端生成一个 32 位半掩码，其中 1 的数量由模式决定，再将其重复一次组成初始 64 位掩码。第 0 个数据块直接使用该掩码。第 `i` 个数据块按照 `low entropy mask rotation` 编码的方向，将初始掩码旋转 `i * R` 位；每次旋转都从初始掩码计算。

每个源数据块按大端字节序解释，并放入源数值的低位。发送端把这些位存入当前掩码值为 1 的位置。其余所有位置都使用同一个填充值。该值可以是 0 或 1，并且对于给定的发送主机和 mieru 版本保持稳定。最后一个不完整数据块中，掩码选中但未被实际数据使用的位置也属于填充。

接收端从第一个数据块推断填充值，并要求所有数据块中的每个非数据位置都使用该值。混合填充、无效模式或旋转、掩码中的 1 位数错误，以及编码长度与提取长度不一致，都会使数据段无效。对掩码选中的密文位或保持不变的认证标签所做的修改，则会被 AEAD 认证拒绝。

所有多字节元数据字段及编码后的 64 位数据块在线路上均使用大端字节序。下面是 `LOW_ENTROPY_MODE_32` 模式的完整示例：

```text
半掩码：                 0x0f0f0f0f
64 位掩码：              0x0f0f0f0f0f0f0f0f
旋转：                   0
源数据：                 12 34 56 78
填充值为 0 的编码结果：  01 02 03 04 05 06 07 08
填充值为 1 的编码结果：  f1 f2 f3 f4 f5 f6 f7 f8
```

## UDP Associate 的封装

mieru 支持使用 TCP 和 UDP 代理协议传输 socks5 UDP associate 请求。为了保留 socks5 UDP 数据包的边界，mieru 会对原始 UDP associate 数据包进行如下的封装：

| marker 1 | data length | data | marker 2 |
| :----: | :----: | :----: | :----: |
| 1 | 2 | X | 1 |

其中 `marker 1` 的值恒定为 `0x00`，`data length` 的值为 `X`，`marker 2` 的值恒定为 `0xff`。封装后的结果将作为原始数据交给 TCP 和 UDP 代理协议进行加密和传输。
