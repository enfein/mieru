# Server Installation & Configuration

The proxy server software mita needs to run on Linux. We provide both debian and RPM installers for installing mita on Debian / Ubuntu and Fedora / CentOS / Red Hat Enterprise Linux series distributions.

Before installation and configuration, connect to the server via SSH and then execute the following commands.

## Download mita installation package

```sh
# Debian / Ubuntu - X86_64
curl -LSO https://github.com/enfein/mieru/releases/download/v1.15.1/mita_1.15.1_amd64.deb

# Debian / Ubuntu - ARM 64
curl -LSO https://github.com/enfein/mieru/releases/download/v1.15.1/mita_1.15.1_arm64.deb

# Fedora / CentOS / Red Hat Enterprise Linux - X86_64
curl -LSO https://github.com/enfein/mieru/releases/download/v1.15.1/mita-1.15.1-1.x86_64.rpm

# Fedora / CentOS / Red Hat Enterprise Linux - ARM 64
curl -LSO https://github.com/enfein/mieru/releases/download/v1.15.1/mita-1.15.1-1.aarch64.rpm
```

If the above link is blocked, please use your browser to download and install from the GitHub Releases page.

## Install mita package

```sh
# Debian / Ubuntu - X86_64
sudo dpkg -i mita_1.15.1_amd64.deb

# Debian / Ubuntu - ARM 64
sudo dpkg -i mita_1.15.1_arm64.deb

# Fedora / CentOS / Red Hat Enterprise Linux - X86_64
sudo rpm -Uvh --force mita-1.15.1-1.x86_64.rpm

# Fedora / CentOS / Red Hat Enterprise Linux - ARM 64
sudo rpm -Uvh --force mita-1.15.1-1.aarch64.rpm
```

## Grant permissions

```sh
sudo usermod -a -G mita $USER

sudo reboot
```

## After reboot, reconnect the server via SSH, check mita daemon status

```sh
systemctl status mita
```

If the output contains `active (running)`, it means that the mita daemon is already running. Normally, mita will start automatically after the server is booted.

## Check mita working status

```sh
mita status
```

If the installation is just completed, the output will be `mieru server status is "IDLE"`, indicating that mita has not yet listening to requests from the mieru client.

## Modify proxy server settings

The mieru proxy supports two different transport protocols, TCP and UDP. To understand the differences between the protocols, please read [mieru proxy protocols](https://github.com/enfein/mieru/blob/main/docs/protocol.md).

Users should call

```sh
mita apply config <FILE>
```

to modify the proxy server settings. `<FILE>` is a JSON formatted configuration file. Below is an example of the server configuration file.

```js
{
    "portBindings": [
        {
            "port": 2012,
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

1. The `portBindings` -> `port` property is the TCP or UDP port number that mita listens on, specify a value from 1025 to 65535. **Please make sure that the firewall allows communication using this port.**
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

to check working status. If `mieru server status is "RUNNING"` is returned here, it means that the proxy service is running and can process client requests.

If you want to stop the proxy service, use command

```sh
mita stop
```

Note that each time you change the settings with `mita apply config <FILE>`, you need to restart the service with `mita stop` and `mita start` for the new settings to take effect.

After starting the proxy service, proceed to [Client Installation & Configuration](https://github.com/enfein/mieru/blob/main/docs/client-install.md).

## [Optional] Install NTP network time synchronization service

The client and proxy server software calculate the key based on the user name, password and system time. The server can decrypt and respond to the client's request only if the client and server have the same key. This requires that the system time of the client and the server must be in sync.

To ensure that the server system time is accurate, we recommend that users install the NTP network time service. In many Linux distributions, installing NTP is only one command.

```sh
# Debian / Ubuntu
sudo apt-get install ntp

# Fedora
sudo dnf install ntp

# CentOS / Red Hat Enterprise Linux
sudo yum install ntp
```
