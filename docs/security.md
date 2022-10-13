# Security Guide

This article focuses on how to safely use mieru. **Some content may be specific to China.**

## Do NOT use domestic operating systems and domestic browsers

Some domestic browsers, such as 360 Safe Browser and QQ Browser, record and upload the use of browser plugins and web proxies. Using a domestic operating system and a domestic browser notifies the police directly.

## Separate domestic traffic from international traffic

Users should not use proxies to access domestic websites. This practice can reduce performance and help GFW discover and block the proxy server. For example, if bilibili or GFW finds that a lot of video playback requests originate from a cloud service provider's IP, that IP may be judged to be used by proxy and be blocked.

mieru does not provide the ability to determine if traffic should be proxied based on rules or IP address locations. Users should use a browser plugin that can be configured for this purpose, such as [Proxy SwitchyOmega](https://github.com/FelisCatus/SwitchyOmega), or use two different browsers, one dedicated to accessing domestic websites and the other dedicated to international traffic.

## Use a virtual machine or a separate computer

Another type of attack occurs on the user's computer. Some heavily installed software, such as QQ, scans processes and files on the computer. If they find users running well-known proxy softwares such as shadowsocks, they will record and report these actions.

Considering that it is difficult for domestic users to not use domestic software at all, we recommend creating a virtual machine, or using a separate computer.

## Use Tor browser

Anti-censorship software developers, investigative journalists, human rights lawyers, and people who engage in political discussions on foreign websites are all high-risk users. In addition to the above suggestions, please make sure to use [Tor](https://www.torproject.org/) for sensitive operations. You cannot connect directly to the Tor bridge in China, but mieru can be used as a Tor proxy. To use a proxy in the Tor browser, please follow the pictures below. Make sure to use the port number that mieru is listening to on your computer.

![Configuring Tor browser](https://github.com/enfein/mieru/blob/main/docs/assets/config_tor_browser.png)

![Configuring Tor browser](https://github.com/enfein/mieru/blob/main/docs/assets/config_tor_browser_2.png)

When using Tor, the internet speed is slow, and you may not be able to watch videos smoothly.
