# Maintenance & Troubleshooting

## View the current connections between client and server

You can run `mieru get connections` command on the client to view the current connections between client and server. An example of the command output is as follows.

```
Session ID  Protocol  Local       Remote        State        Recv Q+Buf  Send Q+Buf  Last Recv  Last Send
2187011369  UDP       [::]:59998  1.2.3.4:5678  ESTABLISHED  0+0         0+1         1s         1s
1466481848  UDP       [::]:59999  1.2.3.4:5678  ESTABLISHED  0+0         0+1         3s         3s
```

Similarly, you can run `mita get connections` command on the server to view the current connections between the server and all clients.

## Configuration file location

The configuration of the mita proxy server is stored in `/etc/mita/server.conf.pb`. This is a binary file in protocol buffer format. To protect user information, mita does not store the user's password in plain text, it only stores the checksum.

The configuration file of the mieru client is also a binary file. It is stored in the following locations in different operating systems.

| Operating System | Configuration File Path | Example Path |
| :----: | :----: | :----: |
| Linux | $HOME/.config/mieru/client.conf.pb | /home/enfein/.config/mieru/client.conf.pb |
| Mac OS | $HOME/Library/Application Support/mieru/client.conf.pb | /Users/enfein/Library/Application Support/mieru/client.conf.pb |
| Windows | %USERPROFILE%\AppData\Roaming\mieru\client.conf.pb | C:\Users\enfein\AppData\Roaming\mieru\client.conf.pb |

## View mita proxy server log

The user can print the full log of mita proxy server using the following command.

```sh
sudo journalctl -u mita -xe --no-pager
```

## View mieru client log

The location of the client mieru log files is shown in the following table.

| Operating System | Log Files Path | Example Path |
| :----: | :----: | :----: |
| Linux | $HOME/.cache/mieru/ or $XDG_CACHE_HOME/mieru/ | /home/enfein/.cache/mieru/ |
| Mac OS | $HOME/Library/Caches/mieru/ | /Users/enfein/Library/Caches/mieru/ |
| Windows | %USERPROFILE%\AppData\Local\mieru\ | C:\Users\enfein\AppData\Local\mieru\ |

Each log file uses the format `yyyyMMdd_HHmm_PID.log`, where `yyyyMMdd_HHmm` is the time when the mieru process was started and `PID` is the process number. Each time mieru is restarted, a new log file is generated. When there are too many log files, the old ones will be deleted automatically.

## Enable and disable debug logging

mieru / mita prints very little information at the default log level, which does not contain sensitive information such as IP addresses, port numbers, etc. If you need to diagnose a single network connection, you need to turn on the debug logging.

The project provides configuration files for quickly enable and disable debug logs in the `configs/templates` directory. Please download them to your server or local computer and enter the following commands. Note that changing the settings of mieru requires restarting the service to take effect.

```sh
# enable debug logging
mita apply config server_enable_debug_logging.json

# OR disable debug logging
mita apply config server_disable_debug_logging.json

# This will not interrupt traffic
mita reload
```

```sh
# enable debug logging
mieru apply config client_enable_debug_logging.json

# OR disable debug logging
mieru apply config client_disable_debug_logging.json

mieru stop

mieru start
```

## Check connectivity between client and server

To determine if the connectivity is OK, you can look at the client metrics. To get the metrics, run command `mieru get metrics`. In the following example,

```json
{
    "cipher - client": {
        "DirectDecrypt": 25683,
        "FailedDirectDecrypt": 0
    },
    "connections": {
        "ActiveOpens": 2,
        "CurrEstablished": 2,
        "MaxConn": 2,
        "PassiveOpens": 0
    },
    "HTTP proxy": {
        "ConnErrors": 0,
        "Requests": 2,
        "SchemeErrors": 0
    },
    "replay": {
        "KnownSession": 0,
        "NewSession": 0
    },
    "socks5": {
        "ConnectionRefusedErrors": 0,
        "DNSResolveErrors": 0,
        "HandshakeErrors": 0,
        "HostUnreachableErrors": 0,
        "NetworkUnreachableErrors": 0,
        "UDPAssociateErrors": 0,
        "UnsupportedCommandErrors": 0
    },
    "socks5 UDP associate": {
        "InBytes": 0,
        "InPkts": 0,
        "OutBytes": 0,
        "OutPkts": 0
    },
    "traffic": {
        "InBytes": 14680428,
        "OutBytes": 1853242,
        "OutPaddingBytes": 271334
    },
    "underlay": {
        "ActiveOpens": 2,
        "CurrEstablished": 2,
        "MaxConn": 2,
        "PassiveOpens": 0,
        "UnderlayMalformedUDP": 0,
        "UnsolicitedUDP": 0
    }
}
```

if the value of `connections` -> `CurrEstablished` is not 0, there is an active connection between the client and the server at this moment; if the value of `cipher - client` -> `DirectDecrypt` is not 0, the client has successfully decrypted the response packets sent by the server.

## Troubleshooting suggestions

mieru enhances server-side stealth in order to prevent GFW active probing, but it also makes debugging more difficult. If you cannot establish a connection between your client and server, it may be helpful to start with the following steps.

1. Is the server working properly? Use the `ping` command to confirm whether the client can reach the server.
2. Is the mita proxy service started? Use the `mita status` command to check.
3. Is the mieru client started? Use the `mieru status` command to check.
4. Are the port numbers of the client and server settings the same? Is the username the same? Is the password the same? Use `mieru describe config` and `mita describe config` to check.
5. Open the debug logs for both the client and server to see exactly what is happening with your network connection.

If you can't solve the problem, you can submit a GitHub issue to contact the developers.

## Reset Server Metrics

Server metrics are stored in the file `/var/lib/mita/metrics.pb`. If you want to reset the metrics, you can run the following command:

```sh
sudo systemctl stop mita && sudo rm -f /var/lib/mita/metrics.pb && sudo systemctl start mita
```

## Environment Variables

If necessary, you can use environment variables to control the behavior of the server and the client.

- `MITA_CONFIG_JSON_FILE` loads the JSON server configuration file from this path.
- `MITA_CONFIG_FILE` loads the protocol buffer server configuration file from this path.
- `MIERU_CONFIG_JSON_FILE` loads the JSON client configuration file from this path. Typically used to run multiple client processes simultaneously.
- `MIERU_CONFIG_FILE` loads the protocol buffer client configuration file from this path.
- If `MITA_LOG_NO_TIMESTAMP` is not empty, the server log does not print timestamps. Since journald already provides timestamps, we enable this by default to avoid printing duplicate timestamps.
- `MITA_UDS_PATH` creates the server UNIX domain socket file using this path. The default path is `/var/run/mita.sock`.
- If `MITA_INSECURE_UDS` is not empty, do not enforce the user and access rights to the server UNIX domain socket file `/var/run/mita.sock`. This setting can be used on systems that are very restricted (e.g., cannot create new users).
