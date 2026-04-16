# mieru 中的 Noise Protocol Framework

从 v3.31.0 开始,mieru 可以使用完整的 [Noise Protocol
Framework](https://noiseprotocol.org) 握手替代默认的基于密码的
XChaCha20-Poly1305 加密方案。Noise 为 mieru 带来:

- **真正的临时 Diffie-Hellman**: 每次连接都产生前向安全的会话密钥,即使
  静态密码后续泄露也无法解密历史流量。
- **可选择的加密套件**: AEAD 密码、哈希函数、握手模式均可在配置中选择。
  套件以 Noise 规范命名表达, 如 `Noise_XX_25519_ChaChaPoly_SHA256`。
- **双向认证**: 当所选模式包含双方静态密钥时(XX、IK、IX 等)。

Noise 是可选的。若 `encryption` 未设置, mieru 继续使用
[protocol.md](protocol.md) 描述的经典协议。Noise 按 profile 和 server
逐一配置, 因此同一程序可以在不同端口同时提供两种方案。

## 快速开始

1. 在客户端和服务器上分别生成长期静态密钥对:

    ```sh
    mieru keypair noise
    # 输出十六进制编码的私钥和公钥
    ```

    服务器侧使用 `mita keypair noise`。

2. 将服务器公钥告知客户端:

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
                    "localStaticPrivateKey": "<客户端私钥, hex>",
                    "remoteStaticPublicKey": "<服务器公钥, hex>"
                }
            }
        ],
        "activeProfile": "default",
        "rpcPort": 8989,
        "socks5Port": 1080
    }
    ```

3. 将服务器静态密钥告知服务器:

    ```json
    {
        "portBindings": [{ "port": 8964, "protocol": "TCP" }],
        "users": [ { "name": "baozi", "password": "manlianpenfen" } ],
        "encryption": "NOISE",
        "noise": {
            "pattern":                "NOISE_XX",
            "localStaticPrivateKey":  "<服务器私钥, hex>"
        }
    }
    ```

使用 XX 模式时, 服务器**无需**预先知道客户端静态密钥 — 握手过程中会传输。
若想让服务器主动拒绝未知客户端, 改用 IK 模式。

## 配置参考

`encryption` 可取以下值:

| 取值                   | 含义                                                      |
| ---------------------- | --------------------------------------------------------- |
| `XCHACHA20_POLY1305`   | 默认的基于密码的方案。`noise` 字段被忽略。                |
| `NOISE`                | 启用 Noise 握手。`noise` 必须完整且合法。                 |

`noise` 字段:

| 字段                      | 类型             | 是否必填                                      | 说明 |
| ------------------------- | ---------------- | --------------------------------------------- | ---- |
| `pattern`                 | `NoisePattern`   | 建议                                          | 握手模式 (详见下表)。默认 `NOISE_XX`。 |
| `dh`                      | `NoiseDH`        | 否                                            | DH 函数。默认 `NOISE_DH_25519`。 |
| `cipher`                  | `NoiseCipher`    | 否                                            | AEAD 密码。默认 `NOISE_CIPHER_CHACHA20POLY1305`。 |
| `hash`                    | `NoiseHash`      | 否                                            | 哈希函数。默认 `NOISE_HASH_SHA256`。 |
| `localStaticPrivateKey`   | hex 字符串       | 所选模式对本方包含静态密钥时                  | 32 字节的私钥, hex 编码。 |
| `localStaticPublicKey`    | hex 字符串       | 否                                            | 省略时由 `localStaticPrivateKey` 自动推导。 |
| `remoteStaticPublicKey`   | hex 字符串       | 发起方需要预先知道远端静态密钥时              | NK, XK, IK, K, N, X 等模式下发起方必填。 |
| `presharedKey`            | hex 字符串       | 需要 `pskN` 变体时                            | 32 字节 PSK, hex 编码。 |
| `presharedKeyPlacement`   | int32            | 设置 `presharedKey` 后                        | PSK 的位置, 例如 `3` 对应 `XXpsk3`。 |
| `prologue`                | hex 字符串       | 否                                            | 混入握手哈希的任意字节。 |

### 支持的握手模式

mieru 暴露了 Noise 规范 §7 定义的全部 12 种模式:

| 名称      | 认证类型     | 0-RTT | 说明                                            |
| --------- | ------------ | ----- | ----------------------------------------------- |
| `NOISE_NN`| 无           | 否    | 仅用于测试。可被主动中间人攻击。                |
| `NOISE_NK`| 仅服务器     | 是    | 客户端已知服务器静态密钥。                      |
| `NOISE_NX`| 仅服务器     | 否    | 服务器在第 2 条消息中传输静态密钥。             |
| `NOISE_XN`| 仅客户端     | 否    | 客户端在第 3 条消息中传输静态密钥。             |
| `NOISE_XK`| 双向         | 否    | 客户端已知服务器静态密钥。                      |
| `NOISE_XX`| 双向         | 否    | **推荐默认**。                                  |
| `NOISE_IN`| 仅客户端     | 否    | 客户端静态密钥在第 1 条消息中明文传输。         |
| `NOISE_IK`| 双向         | 是    | 客户端已知服务器静态密钥。1-RTT。               |
| `NOISE_IX`| 双向         | 否    | 客户端在第 1 条消息中传输静态密钥。             |
| `NOISE_K` | 双向         | 是    | 单向。双方静态密钥均预知。                      |
| `NOISE_N` | 仅服务器     | 是    | 单向。客户端无静态密钥。                        |
| `NOISE_X` | 双向         | 是    | 单向。客户端传输静态密钥。                      |

设置 `presharedKey` + `presharedKeyPlacement` 即切换到 `pskN` 变体。
例如 `pattern = NOISE_XX`、`presharedKey = <32 字节>`、
`presharedKeyPlacement = 3` 得到 `Noise_XXpsk3_25519_ChaChaPoly_SHA256`。

### 支持的加密套件组件

| DH               | Cipher                              | Hash                  |
| ---------------- | ----------------------------------- | --------------------- |
| `NOISE_DH_25519` | `NOISE_CIPHER_CHACHA20POLY1305`     | `NOISE_HASH_SHA256`   |
|                  | `NOISE_CIPHER_AES256GCM`            | `NOISE_HASH_SHA512`   |
|                  |                                     | `NOISE_HASH_BLAKE2S`  |
|                  |                                     | `NOISE_HASH_BLAKE2B`  |

三列任意组合均合法。若需要 Curve448 或其他密码, 请提 issue 说明使用场景。

## 传输格式

Noise 会话分两个阶段:

1. **握手阶段。** 每条握手消息以 2 字节大端长度前缀 + 相应字节数的
   Noise 握手载荷发送。消息数量取决于模式(N/K/X 为 1 条, NK/NN/IK/IX/IN
   为 2 条, XK/XN/XX/NX 为 3 条)。所有握手字节由
   [flynn/noise](https://github.com/flynn/noise) 直接输出, 该库实现了
   Noise 参考状态机。

2. **传输阶段。** 双方完成密钥协商后, 每个方向拥有独立的 AEAD
   `CipherState`, 维护 64 位单调递增 nonce。该 nonce **不会**在线路上
   传输 — 每次发送的仅是 AEAD 输出:

    ```
    | encrypted payload | auth tag |
    | -----------------:| --------:|
    |     N 字节        |  16 B    |
    ```

   对 ChaCha20-Poly1305 和 AES-GCM, 认证标签固定 16 字节。每次
   `Encrypt` / `Decrypt` 都将本地计数器加 1。由于对端计数器随之递增,
   重放消息天然被拒绝。

mieru 的报文格式 (见 [protocol.md](protocol.md)) 仍然封装每个分片。
在 Noise 模式下, 原本由密码派生的 cipher block 被 Noise 派生的 cipher
block 替换; 后者实现同一 `cipher.BlockCipher` 接口, 协议层其余部分保持
不变。

## 安全说明

- **前向安全。** 除 K 族外所有模式都交换至少一对临时密钥,
  因此每次会话都使用新鲜密钥。即便事后静态密钥被完全泄露,
  历史流量依然无法解密。
- **主动中间人。** 以 `N` 结尾的模式 (NN、XN、IN) 不认证服务器,
  仅适合测试或位于已认证通道之下。
- **静态密钥隐私。** NK, XK, IK, K 族模式中发起方在第 1 条消息里将
  公钥相关信息暴露给观察者; 已知服务器公钥集合的观察者可以做连接
  归并。如需隐藏此信息, 使用 psk 变体或 NOISE_XX。
- **通道绑定。** 握手完成后的握手哈希通过
  `transport.ChannelBinding` 暴露, 双方数值相同。若在 Noise 之上再行
  身份验证, 应将挑战绑定到此值。
- **用户提示。** 默认 mieru 方案由每个用户的密码派生独立密钥,
  服务器仅凭 nonce 前缀即可识别用户。Noise 无密码,
  用户识别依赖静态公钥。若想在同一 Noise 端点服务多个用户,
  可选用 IK 模式(服务器直接读取发起方的静态密钥), 或使用 psk 变体
  并以 `cipher.HashPassword(password, user)` 作为 PSK。

## 实现细节

Noise 相关代码位于两个小包中:

- [`pkg/cipher/noise`](../pkg/cipher/noise) — 封装
  [`github.com/flynn/noise`](https://github.com/flynn/noise),
  提供强类型的 `Config`、合法性校验、hex / proto 解码, 以及带长度前缀
  帧的 `Handshake.Run`。
- [`pkg/cipher/noise_cipher.go`](../pkg/cipher/noise_cipher.go) —
  将握手的传输 `CipherState` 适配为 `cipher.BlockCipher`, 与默认
  XChaCha20-Poly1305 的 cipher 共用同一接口。

mieru 协议层不感知底层使用哪种方案; 两种 cipher 实现同一接口。

### 集成测试

`make run-container-test-noise` 构建独立的 OrbStack / Docker 镜像,
运行 Noise 全部单元测试, 以及一次通过 `cipher.NewNoiseBlockCipher`
的本地端到端往返。场景脚本位于
[`test/deploy/noise/`](../test/deploy/noise)。

### 当前集成状态

- 库: 已完成(17 个单元测试, Noise 规范 §7 的 12 种模式均已暴露)。
- 配置: 已完成(`base.proto`、`clientcfg.proto`、`servercfg.proto`,
  以及 JSON 验证和 `appctl` 绑定)。
- CLI: `mieru keypair noise` / `mita keypair noise` 生成一对全新的
  Curve25519 长期静态密钥, 以 hex 输出到标准输出。
- 协议集成: **TCP 与 UDP 均已完成**。TCP 下, mux 在每次新建立的
  连接上执行 Noise 握手, 之后将派生的传输 `CipherState` 对适配为
  两个 `cipher.BlockCipher`, 直接装入流 underlay 的 send/recv 槽位,
  绕过密码派生。UDP 下, mux 用 Noise 层 (`pkg/protocol/noise_udp.go`)
  包装原始的 `net.PacketConn`, 按源地址追踪握手状态, 并使用
  `noise.UnsafeNewCipherState(suite, key, counter)` 加 64 位显式
  每包计数器, 使得收端无状态 — 支持乱序、丢包和 NAT rebinding。
  参见 [`pkg/protocol/noise_wire.go`](../pkg/protocol/noise_wire.go)、
  [`pkg/protocol/noise_udp.go`](../pkg/protocol/noise_udp.go) 以及
  [`pkg/protocol/mux.go`](../pkg/protocol/mux.go) 中的 `usesNoise()`
  分支。
- 端到端: [`test/deploy/noise_e2e/`](../test/deploy/noise_e2e) 构建
  真实的 `mieru` + `mita` 二进制到容器中, 同时使用 Noise_XX JSON
  配置 TCP 与 UDP, 通过两种 Noise 隧道运行 SOCKS5 流量
  (`make run-container-test-noise-e2e`)。
