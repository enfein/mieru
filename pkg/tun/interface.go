package tun

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"time"

	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/tun/wintun"
	"github.com/libp2p/go-netroute"
	"github.com/xjasonlyu/tun2socks/v2/engine"
)

type tunManager interface {
	SetupInterface(dns string) error
	Teardown() error
}

func StartEngine(ctx context.Context, name, socks5Addr, remoteServerIP, userDNS string) error {
	if ip := net.ParseIP(userDNS); ip == nil || ip.To4() == nil {
		userDNS = "8.8.8.8"
	}
	if name == "" {
		name = "mieru_tun0"
	}

	if err := checkTunPrivileges(); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		log.Infof("TUN: Initializing Windows Wintun driver environmant")
		if err := wintun.InitWintun(); err != nil {
			return fmt.Errorf("failed to extract and load wintun.dll: %w", err)
		}
	}

	route, err := netroute.New()
	if err != nil {
		return fmt.Errorf("failed to init netroute: %w", err)
	}

	netIP := net.ParseIP(userDNS)
	physIface, gateway, _, err := route.Route(netIP)
	if err != nil {
		return fmt.Errorf("failed to find system route to proxy: %w", err)
	}

	manager, err := newOSManager(
		remoteServerIP,
		gateway.String(),
		name,
		physIface.Name,
	)
	if err != nil {
		return fmt.Errorf("failed to create os manager: %w", err)
	}

	tunMTU := physIface.MTU - 100
	if tunMTU < 1280 {
		tunMTU = 1400
	}

	tun2socksCfg := engine.Key{
		MTU:        tunMTU,
		Proxy:      "SOCKS5://" + socks5Addr,
		Interface:  "",
		Device:     "tun://" + name,
		UDPTimeout: 0,
	}

	engine.Insert(&tun2socksCfg)

	log.Infof("TUN: Starting tun2socks core engine...")
	go engine.Start()

	defer func() {
		log.Infof("TUN: Tearing down interface and stopping engine...")
		manager.Teardown()
		engine.Stop()
	}()

	if err := configureOSInterface(ctx, name, userDNS, manager); err != nil {
		return err
	}

	log.Infof("TUN: VPN tunnel successfully established and routing traffic!")

	<-ctx.Done()
	return nil
}

func configureOSInterface(ctx context.Context, ifaceName, dnsStr string, manager tunManager) error {
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if _, err := net.InterfaceByName(ifaceName); err == nil {
			if err := manager.SetupInterface(dnsStr); err == nil {
				return nil
			} else {
				log.Warnf("TUN: SetupInterface failed, retrying: %v", err)
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("interface %s did not appear within 10 seconds", ifaceName)
}
