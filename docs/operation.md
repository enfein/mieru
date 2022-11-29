# Maintenance & Troubleshooting

## Configuration file location

The configuration of the mita proxy server is stored in `/etc/mita/server.conf.pb`. This is a binary file in protocol buffer format. To protect user information, mita does not store the user's password in plain text, it only stores the checksum.

The configuration file of the mieru client is also a binary file. It is stored in the following locations in different operating systems.

| Operating System | Configuration File Path |
| :----: | :----: |
| Linux | $HOME/.config/mieru/client.conf.pb |
| Mac OS | /Users/USERNAME/Library/Application Support/mieru/client.conf.pb |
| Windows | C:\Users\USERNAME\AppData\Roaming\mieru\client.conf.pb |

## View mita proxy server log

The user can print the full log of mita proxy server using the following command.

```sh
sudo journalctl -u mita -xe --no-pager
```

## View mieru client log

The location of the client mieru log files is shown in the following table.

| Operating System | Configuration File Path |
| :----: | :----: |
| Linux | $HOME/.cache/mieru/ or $XDG_CACHE_HOME/mieru/ |
| Mac OS | /Users/USERNAME/Library/Caches/mieru/ |
| Windows | C:\Users\USERNAME\AppData\Local\mieru\ |

Each log file uses the format `yyyyMMdd_HHmm_PID.log`, where `yyyyMMdd_HHmm` is the time when the mieru process was started and `PID` is the process number. Each time mieru is restarted, a new log file is generated. When there are too many log files, the old ones will be deleted automatically.

## Enable and disable debug logging

mieru / mita prints very little information at the default log level, which does not contain sensitive information such as IP addresses, port numbers, etc. If you need to diagnose a single network connection, you need to turn on the debug logging.

The project provides configuration files for quickly enable and disable debug logs in the `configs/templates` directory. Please download them to your server or local computer and enter the following commands. Note that changing the settings of mieru / mita requires restarting the service to take effect.

```sh
# enable debug logging
mita apply config server_enable_debug_logging.json

# OR disable debug logging
mita apply config server_disable_debug_logging.json

mita stop

mita start
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

When the server has a single client, to determine if the connectivity is OK, you can look at the server logs. For example, in the following log,

```
INFO [metrics]
INFO [metrics - cipher - server] DirectDecrypt=0 FailedDirectDecrypt=0 FailedIterateDecrypt=0
INFO [metrics - connections] ActiveOpens=0 CurrEstablished=0 MaxConn=0 PassiveOpens=0
INFO [metrics - errors] KCPInErrors=0 KCPReceiveErrors=0 KCPSendErrors=0 TCPReceiveErrors=0 TCPSendErrors=0 UDPInErrors=0
INFO [metrics - KCP] BytesReceived=0 BytesSent=0 EarlyRetransSegs=0 FastRetransSegs=0 InSegs=0 LostSegs=0 OutOfWindowSegs=0 OutSegs=0 RepeatSegs=0 RetransSegs=0
INFO [metrics - replay] KnownSession=0 NewSession=0
INFO [metrics - socks5] ConnectionRefusedErrors=0 DNSResolveErrors=0 HandshakeErrors=0 HostUnreachableErrors=0 NetworkUnreachableErrors=0 UDPAssociateErrors=0 UDPAssociateInBytes=0 UDPAssociateInPkts=0 UDPAssociateOutBytes=0 UDPAssociateOutPkts=0 UnsupportedCommandErrors=0
INFO [metrics - traffic] InBytes=0 OutBytes=0 OutPaddingBytes=0
```

if the value of `CurrEstablished` is not 0, there is an active connection between the server and the client at this moment; if the value of `DirectDecrypt` is not 0, the server has successfully decrypted the packets sent by the client; if the value of `OutBytes` is not 0, the server has sent a packet to the client.

## Troubleshooting suggestions

mieru enhances server-side stealth in order to prevent GFW active probing, but it also makes debugging more difficult. If you cannot establish a connection between your client and server, it may be helpful to start with the following steps.

1. Is the server working properly? Use the `ping` command to confirm whether the client can reach the server.
2. Is the mita proxy service started? Use the `mita status` command to check.
3. Is the mieru client started? Use the `mieru status` command to check.
4. Are the port numbers of the client and server settings the same? Is the username the same? Is the password the same? Use `mieru describe config` and `mita describe config` to check.
5. Open the debug logs for both the client and server to see exactly what is happening with your network connection.

If you can't solve the problem, you can submit a GitHub issue to contact the developers.
