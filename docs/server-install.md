# Server Installation & Configuration

The proxy server software mita needs to run on Linux. We provide both debian and RPM installers for installing mita on Debian / Ubuntu and Fedora / CentOS / Red Hat Enterprise Linux series distributions.

Before installation and configuration, connect to the server via SSH and then execute the following commands.

## Download mita installation package

```sh
# Debian / Ubuntu - X86_64
curl -LSO https://github.com/enfein/mieru/releases/download/v3.3.3/mita_3.3.3_amd64.deb

# Debian / Ubuntu - ARM 64
curl -LSO https://github.com/enfein/mieru/releases/download/v3.3.3/mita_3.3.3_arm64.deb

# RedHat / CentOS / Rocky Linux - X86_64
curl -LSO https://github.com/enfein/mieru/releases/download/v3.3.3/mita-3.3.3-1.x86_64.rpm

# RedHat / CentOS / Rocky Linux - ARM 64
curl -LSO https://github.com/enfein/mieru/releases/download/v3.3.3/mita-3.3.3-1.aarch64.rpm
```

## Install mita package

```sh
# Debian / Ubuntu - X86_64
sudo dpkg -i mita_3.3.3_amd64.deb

# Debian / Ubuntu - ARM 64
sudo dpkg -i mita_3.3.3_arm64.deb

# RedHat / CentOS / Rocky Linux - X86_64
sudo rpm -Uvh --force mita-3.3.3-1.x86_64.rpm

# RedHat / CentOS / Rocky Linux - ARM 64
sudo rpm -Uvh --force mita-3.3.3-1.aarch64.rpm
```

Those instructions can also be used to upgrade the version of mita software package.

## Grant permissions, logout and login again to make the change effective

```sh
sudo usermod -a -G mita $USER

# logout
exit
```

## Reconnect the server via SSH, check mita daemon status

```sh
systemctl status mita
```

If the output contains `active (running)`, it means that the mita daemon is already running. Normally, mita will start automatically after the server is booted.

## Check mita working status

```sh
mita status
```

If the installation is just completed, the output will be `mita server status is "IDLE"`, indicating that mita has not yet listening to requests from the mieru client.

## Modify proxy server settings

The mieru proxy supports two different transport protocols, TCP and UDP. To understand the differences between the protocols, please read [mieru proxy protocols](./protocol.md).

Users should call

```sh
mita apply config <FILE>
```

to modify the proxy server settings. `<FILE>` is a JSON formatted configuration file. Below is an example of the server configuration file.

```js
{
    "portBindings": [
        {
            "portRange": "2012-2022",
            "protocol": "TCP"
        },
        {
            "port": 2027,
            "protocol": "TCP"
        }
    ],
    "users": [
        {
            "name": "ducaiguozei",
            "password": "xijinping"
        },
        {
            "name": "meiyougongchandang",
            "password": "caiyouxinzhongguo"
        }
    ],
    "loggingLevel": "INFO",
    "mtu": 1400
}
```

1. The `portBindings` -> `port` property is the TCP or UDP port number that mita listens on, specify a value from 1025 to 65535. If you want to listen to a range of consecutive port numbers, you can also use the `portRange` property instead. **Please make sure that the firewall allows communication using these ports.**
2. The `portBindings` -> `protocol` property can be set to `TCP` or `UDP`.
3. Fill in the `users` -> `name` property with the user name.
4. Fill in the `users` -> `password` property with the user's password.
5. The `mtu` property is the maximum data link layer payload size when using the UDP proxy protocol. The default value is 1400. You can choose a value between 1280 and 1500.

In addition to this, mita can listen to several different ports. We recommend using multiple ports in both server and client configurations.

You can also create multiple users if you want to share the proxy for others to use.

Assuming that on the server, the configuration file name is `server_config.json`, call the command `mita apply config server_config.json` to write the configuration after the file is modified.

If there is an error in the configuration, mita will print the problem that occurred. Follow the prompts to modify the configuration file and re-run the `mita apply config <FILE>` command to write the configuration.

After that, invoke command

```sh
mita describe config
```

to check the current proxy settings.

## Start proxy service

Use command

```sh
mita start
```

to start proxy service. At this point, mita will start listening to the port number specified in the settings.

Then, use command

```sh
mita status
```

to check working status. If `mita server status is "RUNNING"` is returned here, it means that the proxy service is running and can process client requests.

If you want to stop the proxy service, use command

```sh
mita stop
```

Note that each time you change the settings with `mita apply config <FILE>`, you need to restart the service with `mita stop` and `mita start` for the new settings to take effect. An exception is, if you only change `users` or `loggingLevel` settings, you may run `mita reload` to load the new settings, which will not disturb active connections between server and client.

After starting the proxy service, proceed to [Client Installation & Configuration](./client-install.md).

## Advanced Settings

### BBR Congestion Control Algorithm

[BBR](https://en.wikipedia.org/wiki/TCP_congestion_control#TCP_BBR) is a congestion control algorithm that does not rely on packet loss. Under poor network conditions, network transmission using BBR is faster than traditional algorithms.

mieru's UDP transmission protocol already uses the BBR algorithm.

Under the project root directory, we provide a script `tools/enable_tcp_bbr.py`, which allows users to enable BBR algorithm on TCP transmission protocol on Linux.

```sh
sudo ./tools/enable_tcp_bbr.py
```

That script can be used on both server side and client side.

### Configuring Outbound Proxy

The outbound proxy feature allows mieru to work with other proxy tools to form a proxy chain. An example of the network topology of a proxy chain is shown in the diagram below:

```
mieru client -> GFW -> mita server -> cloudflare proxy client -> cloudflare CDN -> target website
```

Through proxy chain, the target website sees the IP address of the cloudflare CDN, not the address of the mita server.

Below is an example to configure a proxy chain.

```js
{
    "portBindings": [
        {
            "portRange": "2012-2022",
            "protocol": "TCP"
        },
        {
            "port": 2027,
            "protocol": "TCP"
        }
    ],
    "users": [
        {
            "name": "ducaiguozei",
            "password": "xijinping"
        },
        {
            "name": "meiyougongchandang",
            "password": "caiyouxinzhongguo"
        }
    ],
    "loggingLevel": "INFO",
    "mtu": 1400,
    "egress": {
        "proxies": [
            {
                "name": "cloudflare",
                "protocol": "SOCKS5_PROXY_PROTOCOL",
                "host": "127.0.0.1",
                "port": 4000
            }
        ],
        "rules": [
            {
                "ipRanges": ["*"],
                "domainNames": ["*"],
                "action": "PROXY",
                "proxyName": "cloudflare"
            }
        ]
    }
}
```

1. In the `egress` -> `proxies` property, list the information of outbound proxy servers. The current version only supports socks5 outbound, so the value of `protocol` must be set to `SOCKS5_PROXY_PROTOCOL`.
2. In the `egress` -> `rules` property, list outbound rules. The current version allows users to add up to one rule, and the values of `ipRanges`, `domainNames`, and `action` must be the same as the example above. `proxyName` needs to point to a proxy that exists in `egress` -> `proxies` property.

If you want to turn off the outbound proxy feature, simply set the `egress` property to an empty value `{}`.

Note that proxy chain is different from nested proxy. An example of the network topology of a nested proxy is shown in the diagram below:

```
Tor browser -> mieru client -> GFW -> mita server -> Tor network -> target website
```

For information on how to configure nested proxy on a Tor browser, please refer to the [Security Guide](./security.md).

### Limiting User Traffic

We can use the `users` -> `quotas` property to limit the amount of traffic a user is allowed to use. For example, if you want user "ducaiguozei" to use no more than 1 GB of traffic within 1 day, and no more than 10 GB within 30 days, you can apply the following settings.

```js
{
    "portBindings": [
        {
            "portRange": "2012-2022",
            "protocol": "TCP"
        },
        {
            "port": 2027,
            "protocol": "TCP"
        }
    ],
    "users": [
        {
            "name": "ducaiguozei",
            "password": "xijinping",
            "quotas": [
                {
                    "days": 1,
                    "megabytes": 1024
                },
                {
                    "days": 30,
                    "megabytes": 10240
                }
            ]
        },
        {
            "name": "meiyougongchandang",
            "password": "caiyouxinzhongguo"
        }
    ],
    "loggingLevel": "INFO",
    "mtu": 1400
}
```

## [Optional] Install NTP network time synchronization service

The client and proxy server software calculate the key based on the user name, password and system time. The server can decrypt and respond to the client's request only if the client and server have the same key. This requires that the system time of the client and the server must be in sync.

To ensure that the server system time is accurate, we recommend that users install the NTP network time service. In many Linux distributions, installing NTP is only one command.

```sh
# Debian / Ubuntu
sudo apt-get install ntp

# RedHat / CentOS / Rocky Linux
sudo dnf install ntp
```
