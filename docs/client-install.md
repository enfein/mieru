# Client Installation & Configuration

## Download mieru client

The mieru client supports Windows, Mac OS, and Linux. Users can download it from the GitHub Releases page. After unzip, place the mieru executable under the system path `PATH`.

If your client OS is Linux, you can also install mieru using the debian and RPM installers.

## Modify proxy client settings

Use can invoke command

```sh
mieru apply config <FILE>
```

to modify the proxy client settings. Here `<FILE>` is a JSON formatted file. We provide a configuration template in the `configs/templates/client_config.json` file in the root of the project. The contents of this template are as follows.

```js
{
    "profiles": [
        {
            "profileName": "default",
            "user": {
                "name": "<username@example.com>",
                "password": "<your-password>"
            },
            "servers": [
                {
                    "ipAddress": "<1.1.1.1>",
                    "domainName": "",
                    "portBindings": [
                        {
                            "port": -1,
                            "protocol": "TCP"
                        }
                    ]
                }
            ],
            "mtu": 1400
        }
    ],
    "activeProfile": "default",
    "rpcPort": -1,
    "socks5Port": -1,
    "loggingLevel": "INFO",
    "socks5ListenLAN": false,
    "httpProxyPort": -1,
    "httpProxyListenLAN": false
}
```

Please download or copy this template and use a text editor to modify the following fields.

1. In the `profiles` -> `user` -> `name` property, fill in the username. This must be the same as the setting in the proxy server.
2. In the `profiles` -> `user` -> `password` property, fill in the password. This must be the same as the setting in the proxy server.
3. In the `profiles` -> `servers` -> `ipAddress` property, fill in the public address of the proxy server. Both IPv4 and IPv6 addresses are supported.
4. If you have registered a domain name for the proxy server, please fill in the domain name in `profiles` -> `servers` -> `domainName`. Otherwise, do not modify this property.
5. Fill in `profiles` -> `servers` -> `portBindings` -> `port` with the TCP or UDP port number that mita is listening on. The port number must be the same as the one set in the proxy server.
6. Specify a value between 1280 and 1500 for the `profiles` -> `mtu` property. The default value is 1400. This value can be different from the setting in the proxy server.
7. Please specify a value between 1025 and 65535 for the `rpcPort` property.
8. Please specify a value between 1025 and 65535 for the `socks5Port` property. This port cannot be the same as `rpcPort`.
9. If the client needs to provide proxy services to other devices on the LAN, set the `socks5ListenLAN` property to `true`.
10. If you want to enable HTTP / HTTPS proxy, Please specify a value between 1025 and 65535 for the `httpProxyPort` property. This port cannot be the same as `rpcPort` or `socks5Port`. If the client needs to provide HTTP / HTTPS proxy services to other devices on the LAN, set the `httpProxyListenLAN` property to `true`. If you want to disable HTTP / HTTPS proxy, please delete `httpProxyPort` and `httpProxyListenLAN` property.

If you have multiple proxy servers installed, or one server listening on multiple ports, you can add them all to the client settings. Each time a new connection is created, mieru will randomly select one of the servers and one of the ports. **If you are using multiple servers, make sure that each server has the mita proxy service started.**

An example of the above setting is as follows.

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
                    "domainName": "",
                    "portBindings": [
                        {
                            "port": 2027,
                            "protocol": "TCP"
                        }
                    ]
                }
            ],
            "mtu": 1400
        }
    ],
    "activeProfile": "default",
    "rpcPort": 8964,
    "socks5Port": 1080,
    "loggingLevel": "INFO",
    "socks5ListenLAN": false,
    "httpProxyPort": 8080,
    "httpProxyListenLAN": false
}
```

Assuming the file name of this configuration file is `client_config.json`, call command `mieru apply config client_config.json` to write the configuration after it has been modified.

If the configuration is incorrect, mieru will print the problem that occurred. Follow the prompts to modify the configuration file and re-run the `mieru apply config <FILE>` command to write the configuration.

After that, invoke command

```sh
mieru describe config
```

to check the current proxy settings.

## Start proxy client

```sh
mieru start
```

If the output shows `mieru client is started, listening to 127.0.0.1:xxxx`, it means that the mieru client is up and running.

The mieru client will not be started automatically with system boot. After restarting the computer, you need to start the client manually with the `mieru start` command.

**Windows users should note that after starting the client with the `mieru start` command at the command prompt or Powershell, do not close the command prompt or Powershell window. Closing the window will cause the mieru client to exit.**

If you need to stop the mieru client, enter the following command

```sh
mieru stop
```

Note that every time you change the settings with `mieru apply config <FILE>`, you need to restart the client with `mieru stop` and `mieru start` for the new settings to take effect.

## Configuring the browser

Chrome / Firefox and other browsers can use socks5 proxy to access blocked websites by installing browser plugins. For the address of the socks5 proxy, please fill in `127.0.0.1:xxxx`, where `xxxx` is the value of `socks5Port` in the client settings. This address will also be printed when the `mieru start` command is called.

mieru doesn't use socks5 authentication.

For configuring the socks5 proxy in the Tor browser, see the [Security Guide](https://github.com/enfein/mieru/blob/main/docs/security.md).

If you need to forward all application traffic through a proxy, or customize routing rules, use a proxy platform such as clash, and use mieru as the backend of the proxy platform. An example of clash configuration is provided below.

## Configuring clash

In this example, clash listens to port 7890. If the traffic accesses the LAN or if the destination address is in China, then no proxy is used. In other cases, the mieru proxy is used.

```yaml
mixed-port: 7890

proxies:
  - name: mieru
    type: socks5
    server: 127.0.0.1
    port: 1080
    udp: true

dns:
  enable: true
  ipv6: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  nameserver:
    - https://1.1.1.1/dns-query
    - 8.8.8.8

rules:
  - DOMAIN-KEYWORD,google,mieru
  - DOMAIN-KEYWORD,gmail,mieru
  - DOMAIN-KEYWORD,youtube,mieru
  - IP-CIDR,127.0.0.0/8,DIRECT
  - IP-CIDR,172.16.0.0/12,DIRECT
  - IP-CIDR,192.168.0.0/16,DIRECT
  - GEOIP,CN,DIRECT
  - MATCH,mieru
```
