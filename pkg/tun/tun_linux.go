//go:build linux

package tun

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

type linuxManager struct {
	remoteIP net.IP
	gateway  net.IP
	tunName  string
	physName string
}

func newOSManager(remoteIPStr, gatewayStr, tunName, physName string) (tunManager, error) {
	remoteIP := net.ParseIP(remoteIPStr)
	if remoteIP == nil {
		return nil, fmt.Errorf("invalid remote addr (%v)", remoteIPStr)
	}

	gateway := net.ParseIP(gatewayStr)
	if gateway == nil {
		return nil, fmt.Errorf("invalid gateway (%v)", gatewayStr)
	}

	return &linuxManager{
		remoteIP: remoteIP,
		gateway:  gateway,
		tunName:  tunName,
		physName: physName,
	}, nil
}

func (m *linuxManager) SetupInterface(dnsStr string) error {
	// 1. Find our TUN interface
	tunLink, err := netlink.LinkByName(m.tunName)
	if err != nil {
		return fmt.Errorf("failed to find tun interface %s: %w", m.tunName, err)
	}

	// Find the physical interface
	physLink, err := netlink.LinkByName(m.physName)
	if err != nil {
		return fmt.Errorf("failed to find physical interface %s: %w", m.physName, err)
	}

	// FORCE BRING UP THE TUNNEL IN THE OS (ip link set crucian0 up)
	if err := netlink.LinkSetUp(tunLink); err != nil {
		return fmt.Errorf("failed to set tun link up: %w", err)
	}

	// Assign an IP address to the TUN interface
	tunAddr, err := netlink.ParseAddr("10.0.0.2/24")
	if err != nil {
		return err
	}

	if err := netlink.AddrAdd(tunLink, tunAddr); err != nil {
		return fmt.Errorf("failed to add IP to tun: %w", err)
	}

	// Add a route to the proxy server through the physical interface
	proxyRoute := &netlink.Route{
		LinkIndex: physLink.Attrs().Index,
		Dst: &net.IPNet{
			IP:   m.remoteIP,
			Mask: net.CIDRMask(32, 32),
		},
		Gw: m.gateway,
	}
	if err := netlink.RouteAdd(proxyRoute); err != nil {
		return fmt.Errorf("failed to add route to proxy: %w", err)
	}

	// Route all internet traffic through the tunnel
	_, net1, _ := net.ParseCIDR("0.0.0.0/1")
	_, net2, _ := net.ParseCIDR("128.0.0.0/1")

	routesInTun := []*net.IPNet{net1, net2}
	for _, dstNet := range routesInTun {
		r := &netlink.Route{
			LinkIndex: tunLink.Attrs().Index,
			Dst:       dstNet,
		}
		if err := netlink.RouteAdd(r); err != nil {
			return fmt.Errorf("failed to add vpn route %s: %w", dstNet.String(), err)
		}
	}
	return nil
}

func (m *linuxManager) Teardown() error {
	physLink, err := netlink.LinkByName(m.physName)
	if err != nil {
		return err
	}

	proxyRoute := &netlink.Route{
		LinkIndex: physLink.Attrs().Index,
		Dst: &net.IPNet{
			IP:   m.remoteIP,
			Mask: net.CIDRMask(32, 32),
		},
		Gw: m.gateway,
	}
	return netlink.RouteDel(proxyRoute)
}
