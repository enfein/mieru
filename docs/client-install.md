# Client Installation & Configuration

## Download mieru client

The mieru client supports Windows, Mac OS, and Linux. Users can download it from the GitHub Releases page. After unzip, place the mieru executable under the system path `PATH`.

If your client OS is Linux, you can also install mieru using the debian and RPM installers.

## Modify proxy client settings

Use can invoke command

```sh
mieru apply config <FILE>
```

to modify the proxy client settings. `<FILE>` is a JSON formatted configuration file. This configuration file does not need to specify the full proxy client settings. When you run command `mieru apply config <FILE>`, the contents of the file will be merged into any existing proxy client settings.

An example of client configuration is as follows.

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
                            "portRange": "2012-2022",
                            "protocol": "TCP"
                        },
                        {
                            "port": 2027,
                            "protocol": "TCP"
                        }
                    ]
                }
            ],
            "mtu": 1400,
            "multiplexing": {
                "level": "MULTIPLEXING_HIGH"
            }
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

Please use a text editor to modify the following fields.

1. In the `profiles` -> `user` -> `name` property, fill in the username. This must be the same as the setting in the proxy server.
2. In the `profiles` -> `user` -> `password` property, fill in the password. This must be the same as the setting in the proxy server.
3. In the `profiles` -> `servers` -> `ipAddress` property, fill in the public address of the proxy server. Both IPv4 and IPv6 addresses are supported.
4. [Optional] If you have registered a domain name for the proxy server, please fill in the domain name in `profiles` -> `servers` -> `domainName`. Otherwise, do not modify this property.
5. Fill in `profiles` -> `servers` -> `portBindings` -> `port` with the TCP or UDP port number that mita is listening to. The port number must be the same as the one set in the proxy server. If you want to listen to a range of consecutive port numbers, you can also use the `portRange` property instead.
6. [Optional] Specify a value between 1280 and 1400 for the `profiles` -> `mtu` property. The default value is 1400. This value can be different from the setting in the proxy server.
7. [Optional] If you want to adjust the frequency of multiplexing, you can set a value for the `profiles` -> `multiplexing` -> `level` property. The values you can use here include `MULTIPLEXING_OFF`, `MULTIPLEXING_LOW`, `MULTIPLEXING_MIDDLE`, and `MULTIPLEXING_HIGH`. `MULTIPLEXING_OFF` will disable multiplexing, and the default value is `MULTIPLEXING_LOW`.
8. Please specify a value between 1025 and 65535 for the `rpcPort` property.
9. Please specify a value between 1025 and 65535 for the `socks5Port` property. This port cannot be the same as `rpcPort`.
10. [Optional] If the client needs to provide proxy services to other devices on the LAN, set the `socks5ListenLAN` property to `true`.
11. [Optional] If you want to enable HTTP / HTTPS proxy, Please specify a value between 1025 and 65535 for the `httpProxyPort` property. This port cannot be the same as `rpcPort` or `socks5Port`. If the client needs to provide HTTP / HTTPS proxy services to other devices on the LAN, set the `httpProxyListenLAN` property to `true`. If you want to disable HTTP / HTTPS proxy, please delete `httpProxyPort` and `httpProxyListenLAN` property.

If you have multiple proxy servers installed, or one server listening on multiple ports, you can add them all to the client settings. Each time a new connection is created, mieru will randomly select one of the servers and one of the ports. **If you are using multiple servers, make sure that each server has the mita proxy service started.**

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

If the output shows `mieru client is started, listening to xxxxx`, it means that the mieru client is running in the background.

The mieru client will not be started automatically with system boot. After restarting the computer, you need to start the client manually with the `mieru start` command.

**Windows users should note that after starting the client with the `mieru start` command at the command prompt or Powershell, do not close the command prompt or Powershell window. Closing the window will cause the mieru client to exit.** Some new versions of Windows allow users to minimize the command prompt or Powershell to the tray.

If you need to stop the mieru client, enter the following command

```sh
mieru stop
```

Note that every time you change the settings with `mieru apply config <FILE>`, you need to restart the client with `mieru stop` and `mieru start` for the new settings to take effect.

## Test the Connection Between Client and Server

```sh
mieru test

OR

mieru test https://<website.you.want.to.connect>
```

If the output shows `Connected to ...`, it indicates that the mieru client has successfully connected to the proxy server.

## Configuring the browser

Chrome / Firefox and other browsers can use socks5 proxy to access blocked websites by installing browser plugins. For the address of the socks5 proxy, please fill in `127.0.0.1:xxxx`, where `xxxx` is the value of `socks5Port` in the client settings. This address will also be printed when the `mieru start` command is called.

mieru doesn't use socks5 authentication.

For configuring the socks5 proxy in the Tor browser, see the [Security Guide](./security.md).

## Advanced Settings

### socks5 Username and Password Authentication

If you want to require applications to authenticate the socks5 proxy using a username and password, you can add the `socks5Authentication` property to the client configuration. An example is as follows:

```js
{
    "socks5Authentication": [
        {
            "user": "yitukai",
            "password": "manlianpenfen"
        },
        {
            "user": "shilishanlu",
            "password": "buhuanjian"
        }
    ]
}
```

Applications can choose any user and password in the `socks5Authentication` list to authenticate the socks5 proxy.

**socks5 username and password authentication is not compatible with HTTP / HTTPS proxy.** Since HTTP / HTTPS proxy does not require username and password authentication, based on threat model, mieru prohibits the use of HTTP / HTTPS proxy in conjunction with socks5 username and password authentication.

If you need to delete an existing HTTP / HTTPS proxy configuration, please run the `mieru delete http proxy` command. If you want to delete the socks5 username and password authentication settings, please run the `mieru delete socks5 authentication` command.

## Sharing Client Settings

Users can use commands `mieru export config` or `mieru export config simple` to generate URL links to share the client's configuration. These URL links can be imported into other clients using command `mieru import config <URL>`.

### Standard Sharing Link

Use command `mieru export config` to generate a standard sharing link. For example:

```
mieru://CpsBCgdkZWZhdWx0ElgKBWJhb3ppEg1tYW5saWFucGVuZmVuGkA0MGFiYWM0MGY1OWRhNTVkYWQ2YTk5ODMxYTUxMTY1MjJmYmM4MGUzODViYjFhYjE0ZGM1MmRiMzY4ZjczOGE0Gi8SCWxvY2FsaG9zdBoFCIo0EAIaDRACGgk5OTk5LTk5OTkaBQjZMhABGgUIoCYQASD4CioCCAQSB2RlZmF1bHQYnUYguAgwBTgA
```

A standard sharing link starts with `mieru://` and uses base64 encoding for the full client configuration. Users can use the standard sharing link to replicate the client configuration on a brand new device.

### Simple Sharing Link

Use command `mieru export config simple` to generate human-readable simple sharing links. For example:

```
mierus://baozi:manlianpenfen@1.2.3.4?mtu=1400&multiplexing=MULTIPLEXING_HIGH&port=6666&port=9998-9999&port=6489&port=4896&profile=default&protocol=TCP&protocol=TCP&protocol=UDP&protocol=UDP
```

The format of the simple sharing link is as follows:

`mierus://username:password@server_address?parameter_list`

A simple sharing link starts with `mierus://`, where `s` stands for `simple`.

There is only one server address in a simple sharing link. If the client's configuration contains multiple servers, multiple links will be generated.

The supported parameters are:

- `profile`
- `mtu`
- `multiplexing`
- `port`
- `protocol`

Among them, `profile` must appear once, `mtu` and `multiplexing` can appear at most once, `port` and `protocol` can appear multiple times, and they must appear the same number of times, such that the `port` and `protocol` at the same position can be associated. Additionally, `port` can also be used to specify a port range.

The simple sharing link above is equivalent to the following client configuration fragment:

```json
{
    "profileName":  "default",
    "user":  {
        "name":  "baozi",
        "password":  "manlianpenfen"
    },
    "servers":  [
        {
            "ipAddress":  "1.2.3.4",
            "portBindings":  [
                {
                    "port":  6666,
                    "protocol":  "TCP"
                },
                {
                    "protocol":  "TCP",
                    "portRange":  "9998-9999"
                },
                {
                    "port":  6489,
                    "protocol":  "UDP"
                },
                {
                    "port":  4896,
                    "protocol":  "UDP"
                }
            ]
        }
    ],
    "mtu":  1400,
    "multiplexing":  {
        "level":  "MULTIPLEXING_HIGH"
    }
}
```

Note: a simple sharing link does not contain necessary client configurations such as `socks5Port`. Therefore, importing a simple sharing link on a brand new device will fail.
