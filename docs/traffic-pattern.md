# Traffic Pattern

## Overview

The traffic pattern feature allows mieru to modify network traffic characteristics to evade deep packet inspection (DPI) and traffic analysis.

Traffic patterns can be configured independently on the client and server. The client and server do not need to use the same traffic pattern settings.

## Configuration

in the client:

```sh
mieru apply config <FILE>
```

in the server:

```sh
mita apply config <FILE>
```

Then restart the proxy service to make the changes effective.

### Client Configuration Example

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

### Server Configuration Example

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

## Configuration Fields

The `trafficPattern` object supports the following fields:

1. [Optional] `seed` - An integer used to generate stable implicit traffic patterns for fields that are not explicitly set. With the same `seed` and `unlockAll` values, the generated implicit traffic patterns do not change.
2. [Optional] `unlockAll` - A boolean that controls the value range of implicit traffic pattern generation. When set to `true`, implicit patterns can use all possible options. When set to `false` (default), implicit patterns use only limited, conservative options.
3. [Optional] `tcpFragment` - An object that configures TCP fragmentation. This has no impact to UDP proxy protocol.
4. [Optional] `nonce` - An object that configures nonce prefix manipulation.
5. [Optional] `padding` - An object that configures padding length limits for network segments.

## TCP Fragmentation

TCP fragmentation splits some TCP packets into smaller fragments, making traffic harder to analyze. The `tcpFragment` object supports the following fields:

1. [Optional] `enable` - A boolean that enables or disables TCP fragmentation. Default is `false`.
2. [Optional] `maxSleepMs` - An integer specifying the maximum sleep time in milliseconds between sending two fragments. The value must be between 0 and 100. A higher value increases the delay between fragments, which can be more effective at evading analysis but may reduce performance.

Enabling TCP fragmentation may increase network latency.

Example:

```js
"tcpFragment": {
    "enable": true,
    "maxSleepMs": 10
}
```

## Nonce Pattern

The nonce pattern feature manipulates the nonce prefix in encrypted packets. The `nonce` object supports the following fields:

1. [Optional] `type` - The nonce manipulation strategy. Possible values are:
   - `NONCE_TYPE_RANDOM` - Do not make changes to the original random nonce. This is the default.
   - `NONCE_TYPE_PRINTABLE` - Use printable ASCII characters (0x20 to 0x7E).
   - `NONCE_TYPE_PRINTABLE_SUBSET` - Use a pre-defined subset of printable ASCII characters.
   - `NONCE_TYPE_FIXED` - Use a customized nonce prefix from `customHexStrings`. If `customHexStrings` is not set, the behavior is the same as `NONCE_TYPE_RANDOM`.
2. [Optional] `applyToAllUDPPacket` - A boolean. If set to `true`, the pattern applies to every UDP packet. If `false` (default), the pattern only applies to the first UDP packet.
3. [Optional] `minLen` - The minimum number of bytes to manipulate. The value must be between 0 and 12. This field is ignored when `type` is `NONCE_TYPE_RANDOM` or `NONCE_TYPE_FIXED`.
4. [Optional] `maxLen` - The maximum number of bytes to manipulate. The value must be between 0 and 12. This field is ignored when `type` is `NONCE_TYPE_RANDOM` or `NONCE_TYPE_FIXED`.
5. [Optional] `customHexStrings` - A list of hex strings (without the `0x` prefix) that represent customized nonce prefixes. For example, the string `"00010203"` represents a 4-byte nonce prefix `[0, 1, 2, 3]`. Each nonce prefix cannot exceed 12 bytes. When multiple strings are provided, a random one is used each time. This field only has effect when `type` is `NONCE_TYPE_FIXED`.

Example with printable nonce:

```js
"nonce": {
    "type": "NONCE_TYPE_PRINTABLE",
    "applyToAllUDPPacket": true,
    "minLen": 6,
    "maxLen": 8
}
```

Example with fixed nonce prefix:

```js
"nonce": {
    "type": "NONCE_TYPE_FIXED",
    "customHexStrings": ["00010203", "04050607"]
}
```

## Padding Pattern

The padding pattern feature controls how many padding bytes mieru / mita may add to network segments. The `padding` object supports the following fields:

1. [Optional] `maxMiddlePaddingLen` - The maximum number of padding bytes in the middle of a network segment. The value must be between 0 and 255. Set it to `0` to disable middle padding.
2. [Optional] `maxEndPaddingLen` - The maximum number of padding bytes at the end of a network segment. The value must be between 0 and 255. Set it to `0` to disable end padding.

These fields set upper limits. The actual padding length can be smaller due to random selection, MTU limits, packet overhead, and payload size. If a field is not set, mieru uses implicit pattern generation to choose a value.

Example:

```js
"padding": {
    "maxMiddlePaddingLen": 64,
    "maxEndPaddingLen": 128
}
```

## Implicit Pattern Generation

When a traffic pattern is configured, mieru automatically generates values for fields that are not explicitly set. This is called implicit pattern generation.

The `seed` field controls the generation. If `seed` is provided, the generated patterns are stable. If `seed` is not provided, the generated traffic pattern can be different in each machine and in each mieru version.

The `unlockAll` field controls the range of generated values.

Explicitly set fields are never affected by implicit generation. For example, if you set `tcpFragment.enable` to `true`, it will remain `true` regardless of the `seed` and `unlockAll` setting.

## Viewing and Exporting

To view the effective traffic pattern (including both explicit and implicitly values), run

in the client:

```sh
mieru describe effective-traffic-pattern
```

in the server:

```sh
mita describe effective-traffic-pattern
```

You can export the traffic pattern as an encoded base64 string, which can be used by third party applications.

in the client:

```sh
mieru export traffic-pattern
```

in the server:

```sh
mita export traffic-pattern
```
