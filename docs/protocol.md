# mieru 代理协议

mieru 代理协议在原始数据流量的基础上进行了两层封装。第一层封装使用修改后的 [KCP](https://github.com/skywind3000/kcp) 协议，实现了随机数据填充和自动重传 ([ARQ](https://en.wikipedia.org/wiki/Automatic_repeat_request))。关于这部分的介绍，请阅读章节【解密数据包的格式】和【保持连接】。第二层封装使用 [AEAD](https://en.wikipedia.org/wiki/Authenticated_encryption) 算法对数据包进行加密，再通过 UDP 协议发送到互联网上传输。请阅读章节【加密数据包的格式】和【密钥生成方法】了解相关的信息。

## 解密数据包的格式

我们将原始的 socks5 数据流量记为 `data`，其长度为 `X` 个字节。在创建第一层封装时，mieru 可能会在原始数据的末尾填充随机数据。我们将随机数据记为 `padding`，其长度是 `Y` 个字节。解密数据包各部分的内容和长度如下所示：

| KCP header | data | padding |
| :----: | :----: | :----: |
| 24 | X | Y |

其中，`KCP header` 部分的内容和长度如下：

| conversation ID | command | fragment count | receive window size | timestamp | sequence number | unacknowledged sequence number | data length | data + padding length |
| :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: | :----: |
| 4 | 1 | 1 | 2 | 4 | 4 | 4 | 2 | 2 |

注意，与大多数网络协议不同，KCP 使用 little endian 编码多字节的内容。

关于 KCP 协议有很多参考资料，这里不再详细叙述。下面主要讲解 mieru 修改后的 KCP 协议与原协议的差别。

第一，mieru 使用了一组不同的 `command` 数值。在 mieru 中，各个 `command` 指令以及其对应的含义如下：

| command | action |
| :----: | :----: |
| 0x01 | send data |
| 0x02 | acknowledge |
| 0x03 | ask remote window size |
| 0x04 | reply my window size |

第二，原 KCP 协议最后的 4 字节 `length`，在 mieru 中被拆分成了 2 字节的 `data length` 和 2 字节的 `data + padding length`。依据上文的定义，`data length` 的值应为 `X`，`data + padding length` 的值应为 `X + Y`。

## 保持连接

为了及时回收内存资源，服务器会定期清除不活跃的 KCP 会话。如果客户端想保持一个 KCP 会话，需要定期向服务器发送心跳包。心跳包的发送间隔不应大于 30 秒。

## 加密数据包的格式

我们将第一层封装后得到的数据记为 `decrypted`，其长度为 `X + Y + 24`。`decrypted` 经加密后的数据为 `encrypted`，其长度同样为 `X + Y + 24`。第二层封装使用 AEAD 算法对 `decrypted` 进行加密，得到的加密数据包的内容和长度如下所示：

| nonce | encrypted | authentication tag |
| :----: | :----: | :----: |
| 12 | X + Y + 24 | 16 |

加密时可以使用的 AEAD 算法不受限制，只要客户端和服务器使用同一套算法即可。比较流行的算法有 AES-128-GEM, AES-256-GCM, ChaCha20-Poly1305 等。在这个参考实现中，我们使用了 AES-256-GCM 算法。

完成第二层封装之后的数据会以单个 UDP 包的形式发送到互联网上。

## 密钥生成方法

每一个 mieru 用户都需要提供用户名 `username` 和密码 `password`。从用户名和密码生成加密和解密使用的密钥，需要经历如下几步。

第一步，生成一个哈希密码 `hashedPassword`，其值等于 `password` 附加一个 `0x00` 字节再附加 `username` 得到的字符串的 SHA-256 校验码。

第二步，获取系统当前的时间 `unixTime`，其值等于 1970 年 1 月 1 日到现在经历的秒数。将 `unixTime` 的时刻四舍五入到最接近的 5 分钟，以 little endian uint64 存储为一个 8 字节的字符串，取得该字符串的 SHA-256 校验码，记为 `timeSalt`。

第三步，使用 [pbkdf2](https://en.wikipedia.org/wiki/PBKDF2) 算法生成密钥。其中，使用 `hashedPassword` 作为密码，使用 `timeSalt` 作为盐，迭代次数为 4096，密钥长度为 32 字节，哈希算法为 SHA-256。

由于密钥依赖于系统时间，客户端和服务器之间的时间差不能太大。服务器可能需要尝试几组不同的时刻才能顺利解密。
