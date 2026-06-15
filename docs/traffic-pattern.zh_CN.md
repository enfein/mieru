# 流量模式

## 概述

流量模式功能允许 mieru 修改网络流量特征，以规避深度包检测（DPI）和流量分析。

流量模式可以在客户端和服务器上独立配置。客户端和服务器不需要使用相同的流量模式设置。

## 配置方法

客户端：

```sh
mieru apply config <FILE>
```

服务器：

```sh
mita apply config <FILE>
```

然后重启代理服务使更改生效。

### 客户端配置示例

```js
{
    "profiles": [
        {
            "profileName": "default",
            "user": {
                "name": "ducaiguozei",
                "password": "xijinping"
            },
            "servers": [
                {
                    "ipAddress": "12.34.56.78",
                    "portBindings": [
                        {
                            "portRange": "2012-2022",
                            "protocol": "TCP"
                        }
                    ]
                }
            ],
            "trafficPattern": {
                "unlockAll": false,
                "tcpFragment": {
                    "enable": true,
                    "maxSleepMs": 10
                },
                "nonce": {
                    "type": "NONCE_TYPE_PRINTABLE",
                    "applyToAllUDPPacket": true,
                    "minLen": 6,
                    "maxLen": 8
                },
                "padding": {
                    "maxMiddlePaddingLen": 64,
                    "maxEndPaddingLen": 128
                }
            }
        }
    ],
    "activeProfile": "default",
    "rpcPort": 8964,
    "socks5Port": 1080
}
```

### 服务器配置示例

```js
{
    "portBindings": [
        {
            "portRange": "2012-2022",
            "protocol": "TCP"
        }
    ],
    "users": [
        {
            "name": "ducaiguozei",
            "password": "xijinping"
        }
    ],
    "trafficPattern": {
        "unlockAll": true,
        "tcpFragment": {
            "enable": false,
            "maxSleepMs": 0
        },
        "nonce": {
            "type": "NONCE_TYPE_FIXED",
            "customHexStrings": ["00010203", "04050607"]
        },
        "padding": {
            "maxMiddlePaddingLen": 0,
            "maxEndPaddingLen": 255
        }
    }
}
```

## 配置字段说明

`trafficPattern` 对象支持以下字段：

1. 【可选】`seed` - 一个整数，用于为未显式设置的字段生成稳定的隐式流量模式。如果 `seed` 和 `unlockAll` 的值不变，生成的隐式流量模式保持不变。
2. 【可选】`unlockAll` - 一个布尔值，控制隐式流量模式的取值范围。设为 `true` 时，隐式模式可以使用所有可能的选项。设为 `false`（默认）时，隐式模式仅使用有限的保守选项。
3. 【可选】`tcpFragment` - 配置 TCP 分片的对象。它不影响 UDP 代理协议。
4. 【可选】`nonce` - 配置 Nonce 前缀操纵的对象。
5. 【可选】`padding` - 配置网络数据段填充长度限制的对象。

## TCP 分片

TCP 分片功能将某些 TCP 数据包拆分为更小的分片，使流量更难被分析。`tcpFragment` 对象支持以下字段：

1. 【可选】`enable` - 布尔值，启用或禁用 TCP 分片。默认为 `false`。
2. 【可选】`maxSleepMs` - 整数，指定发送两个分片之间的最大休眠时间（毫秒）。值必须在 0 到 100 之间。较高的值会增加分片之间的延迟，可能更有效地规避分析，但会降低性能。

启用 TCP 分片可能会增加网络延迟。

示例：

```js
"tcpFragment": {
    "enable": true,
    "maxSleepMs": 10
}
```

## Nonce 模式

Nonce 模式功能操纵加密数据包中的 Nonce 前缀。`nonce` 对象支持以下字段：

1. 【可选】`type` - Nonce 操纵策略。可选值为：
   - `NONCE_TYPE_RANDOM` - 不修改原始随机 Nonce。这是默认值。
   - `NONCE_TYPE_PRINTABLE` - 使用可打印的 ASCII 字符（0x20 到 0x7E）。
   - `NONCE_TYPE_PRINTABLE_SUBSET` - 使用预定义的可打印 ASCII 字符子集。
   - `NONCE_TYPE_FIXED` - 使用 `customHexStrings` 中的自定义 Nonce 前缀。如果未设置 `customHexStrings`，行为与 `NONCE_TYPE_RANDOM` 相同。
2. 【可选】`applyToAllUDPPacket` - 布尔值。设为 `true` 时，模式应用于每个 UDP 数据包。设为 `false`（默认）时，模式仅应用于第一个 UDP 数据包。
3. 【可选】`minLen` - 要操纵的最小字节数。值必须在 0 到 12 之间。当 `type` 为 `NONCE_TYPE_RANDOM` 或 `NONCE_TYPE_FIXED` 时，此字段将被忽略。
4. 【可选】`maxLen` - 要操纵的最大字节数。值必须在 0 到 12 之间。当 `type` 为 `NONCE_TYPE_RANDOM` 或 `NONCE_TYPE_FIXED` 时，此字段将被忽略。
5. 【可选】`customHexStrings` - 十六进制字符串列表（不含 `0x` 前缀），表示自定义 Nonce 前缀。例如，字符串 `"00010203"` 表示 4 字节的 Nonce 前缀 `[0, 1, 2, 3]`。每个 Nonce 前缀不能超过 12 字节。提供多个字符串时，每次随机选择一个使用。此字段仅在 `type` 为 `NONCE_TYPE_FIXED` 时生效。

使用可打印 Nonce 的示例：

```js
"nonce": {
    "type": "NONCE_TYPE_PRINTABLE",
    "applyToAllUDPPacket": true,
    "minLen": 6,
    "maxLen": 8
}
```

使用固定 Nonce 前缀的示例：

```js
"nonce": {
    "type": "NONCE_TYPE_FIXED",
    "customHexStrings": ["00010203", "04050607"]
}
```

## 填充模式

填充模式功能控制 mieru / mita 会向网络数据段添加多少填充字节。`padding` 对象支持以下字段：

1. 【可选】`maxMiddlePaddingLen` - 网络数据段中间填充字节数的最大值。取值必须在 0 到 255 之间。设为 `0` 可禁用中间填充。
2. 【可选】`maxEndPaddingLen` - 网络数据段末尾填充字节数的最大值。取值必须在 0 到 255 之间。设为 `0` 可禁用末尾填充。

这些字段设置的是上限。实际填充长度可能因为随机选择、MTU 限制、数据包开销和负载大小而更小。如果未设置某个字段，mieru 会使用隐式模式生成来选择一个值。

示例：

```js
"padding": {
    "maxMiddlePaddingLen": 64,
    "maxEndPaddingLen": 128
}
```

## 隐式模式生成

配置流量模式后，mieru 会自动为未显式设置的字段生成值，这称为隐式模式生成。

`seed` 字段控制生成过程。如果提供了 `seed`，生成的模式保持稳定。如果未提供 `seed`，则生成的流量模式在不同机器和不同 mieru 版本之间会不一样。

`unlockAll` 字段控制生成值的范围。

显式设置的字段不会受到隐式生成的影响。例如，如果你将 `tcpFragment.enable` 设为 `true`，无论 `seed` 和 `unlockAll` 的设置如何，它都将保持 `true`。

## 查看和导出流量模式

要查看有效的流量模式（包括显式设置和隐式生成的值），运行：

客户端：

```sh
mieru describe effective-traffic-pattern
```

服务器：

```sh
mita describe effective-traffic-pattern
```

你可以将流量模式导出为 base64 编码的字符串，以便在第三方应用上使用。

客户端：

```sh
mieru export traffic-pattern
```

服务器：

```sh
mita export traffic-pattern
```
