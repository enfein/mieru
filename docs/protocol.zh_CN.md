# mieru 代理协议

为了满足不同场景的需要，mieru 提供了 TCP 和 UDP 两种不同的代理协议。TCP 协议有可能比 UDP 协议更快。不过，与 UDP 协议相比，TCP 协议更有可能被嗅探。

下面是 mieru 代理协议的具体讲解。

## 密钥生成方法

TCP 和 UDP 协议共用同一套密钥生成方法。

每一个 mieru 用户都需要提供用户名 `username` 和密码 `password`。从用户名和密码生成加密和解密使用的密钥，需要经历如下几步。

第一步，生成一个哈希密码 `hashedPassword`，其值等于 `password` 附加一个 `0x00` 字节再附加 `username` 得到的字符串的 SHA-256 校验码。

第二步，获取系统当前的时间 `unixTime`，其值等于 1970 年 1 月 1 日到现在经历的秒数。将 `unixTime` 的时刻四舍五入到最接近的 5 分钟，以 little endian uint64 存储为一个 8 字节的字符串，取得该字符串的 SHA-256 校验码，记为 `timeSalt`。

第三步，使用 [pbkdf2](https://en.wikipedia.org/wiki/PBKDF2) 算法生成密钥。其中，使用 `hashedPassword` 作为密码，使用 `timeSalt` 作为盐，迭代次数为 4096，密钥长度为 32 字节，哈希算法为 SHA-256。

由于密钥依赖于系统时间，客户端和服务器之间的时间差不能太大。服务器可能需要尝试几组不同的时刻才能顺利解密。

## 基于 TCP 的代理协议

TCP 是一种字节流传输协议，应用程序无法感知单个 TCP 数据包的起点和终点。为了便于加密和解密操作，mieru 在字节流中引入了数据段大小标识。数据段的大小与网络中传输的数据包大小无关。

mieru TCP 代理协议在原始数据流量的基础上进行了两层封装。第一层封装，在原始流量的基础上进行随机数据填充，更改数据的长度。关于这部分的介绍，请阅读章节【解密数据段的格式】。第二层封装，使用 [AEAD](https://en.wikipedia.org/wiki/Authenticated_encryption) 算法对数据包进行加密。请阅读章节【加密数据段的格式】了解相关的信息。

### 解密数据段的格式

我们将原始的 socks5 数据流量记为 `data`，其长度为 `X` 个字节。在创建第一层封装时，mieru 可能会在原始数据的末尾填充随机数据。我们将随机数据记为 `padding`，其长度是 `Y` 个字节。解密数据包各部分的内容和长度如下所示：

| data length | data + padding length | data | padding |
| :----: | :----: | :----: | :----: |
| 2 | 2 | X | Y |

这里的 `data length` 的值为 `X`，`data + padding length` 的值为 `X + Y`，他们均使用 little endian 编码。mieru TCP 协议规定 `X + Y` 的值不能大于 16380。不难看出，解密数据段的前四个字节，能告诉程序当前数据段的大小。

### 加密数据段的格式

我们将第一层封装后得到的数据记为 `decrypted`，其长度为 `X + Y + 4`，并将这个长度的值记为 `decrypted length`。该长度值可以用两个字节存储。`decrypted` 经加密后的数据为 `encrypted`，其长度同样为 `X + Y + 4`。`decrypted length` 经加密后的数据为 `encrypted length`，其长度同样为两个字节。

加密数据段的内容和长度如下所示：

| nonce | encrypted length | authentication tag of encrypted length | encrypted | authentication tag of encrypted |
| :----: | :----: | :----: | :----: | :----: |
| 0 或 12 | 2 | 16 | X + Y + 4 | 16 |

其中的 nonce 在 TCP 连接的每个方向（客户端到服务器、服务器到客户端）只会在第一个数据段出现一次。每一个加密数据段传输了两组不同的加密信息，第一组传输两个字节的 `encrypted length`，第二组传输 `X + Y + 4` 字节的 `encrypted` 数据。AEAD 算法会在每一组加密信息后添加 16 字节的验证信息。每传输一组加密信息，nonce 的值会增加 1，变更后的 nonce 将会参与下一组加密或解密的计算。

加密时可以使用的 AEAD 算法不受限制，只要客户端和服务器使用同一套算法即可。比较流行的算法有 AES-128-GEM, AES-256-GCM, ChaCha20-Poly1305 等。在这个实现中，我们使用了 AES-256-GCM 算法。

加密数据段将会被操作系统拆分成大小合适的 TCP 数据包发送到互联网上。

## 基于 UDP 的代理协议

mieru UDP 代理协议在原始数据流量的基础上同样进行了两层封装。第一层封装使用修改后的 [KCP](https://github.com/skywind3000/kcp) 协议，实现了随机数据填充和自动重传 ([ARQ](https://en.wikipedia.org/wiki/Automatic_repeat_request))。关于这部分的介绍，请阅读章节【解密数据包的格式】和【保持连接】。第二层封装使用 AEAD 算法对数据包进行加密，再通过 UDP 数据包发送到互联网上传输。请阅读章节【加密数据包的格式】了解相关的信息。

### 解密数据包的格式

我们将原始的 socks5 数据流量记为 `data`，其长度为 `X` 个字节。在创建第一层封装时，mieru 可能会在原始数据的末尾填充随机数据。我们将随机数据记为 `padding`，其长度是 `Y` 个字节。解密数据包各部分的内容和长度如下所示：

| KCP header | data | padding |
| :----: | :----: | :----: |
| 24 | X | Y |

其中，`KCP header` 部分的内容和长度如下：

| conversation ID | command | fragment count | receive window size | timestamp | sequence number | unacknowledged sequence number | data length | data + padding length |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| 4 | 1 | 1 | 2 | 4 | 4 | 4 | 2 | 2 |

KCP 使用 little endian 编码多字节的内容。关于 KCP 协议的分析，有很多参考资料，这里不再详细叙述。下面主要讲解 mieru 修改后的 KCP 协议与原协议的差别。

第一，mieru 使用了一组不同的 `command` 数值。在 mieru 中，各个 `command` 指令以及其对应的含义如下：

| command | action |
| :----: | :----: |
| 0x01 | send data |
| 0x02 | acknowledge |
| 0x03 | ask remote window size |
| 0x04 | reply my window size |

第二，原 KCP 协议最后的 4 字节 `length`，在 mieru 中被拆分成了 2 字节的 `data length` 和 2 字节的 `data + padding length`。依据上文的定义，`data length` 的值应为 `X`，`data + padding length` 的值应为 `X + Y`。

### 保持连接

为了及时回收内存资源，服务器会定期清除不活跃的 KCP 会话。如果客户端想保持一个 KCP 会话，需要定期向服务器发送心跳包。心跳包的发送间隔不应大于 30 秒。

### 加密数据包的格式

我们将第一层封装后得到的数据记为 `decrypted`，其长度为 `X + Y + 24`。`decrypted` 经加密后的数据为 `encrypted`，其长度同样为 `X + Y + 24`。第二层封装使用 AEAD 算法对 `decrypted` 进行加密，得到的加密数据包的内容和长度如下所示：

| nonce | encrypted | authentication tag |
| :----: | :----: | :----: |
| 12 | X + Y + 24 | 16 |

加密时可以使用的 AEAD 算法不受限制。在这个实现中，我们使用了 AES-256-GCM 算法。

完成第二层封装之后的数据会以**单个 UDP 包**的形式发送到互联网上。因此，第二层封装之后的数据大小，在传输时不应超过当前网络的 MTU 值。mieru UDP 协议要求，单个以太网数据帧的最大负载为 1500 字节。在这个参考实现中，我们使用的默认最大负载是 1400 字节。

## UDP Associate 的封装

mieru 支持使用 TCP 和 UDP 代理协议传输 socks5 UDP associate 请求。为了保留 UDP 数据包的边界，mieru 会对原始 UDP associate 数据包进行如下的封装：

| marker 1 | data length | data | marker 2 |
| :----: | :----: | :----: | :----: |
| 1 | 2 | X | 1 |

其中 `marker 1` 的值恒定为 `0x00`，`data length` 的值为 `X`，使用 little endian 编码，`marker 2` 的值恒定为 `0xff`。封装后的结果将作为原始数据交给 TCP 和 UDP 代理协议进行加密和传输。
