//go:build windows

package tun

import (
	"fmt"
	"net/netip"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

type windowsManager struct {
	remoteIP netip.Addr
	gateway  netip.Addr
	tunName  string
	physName string

	tunLuid  winipcfg.LUID
	physLuid winipcfg.LUID
}

func (w *windowsManager) ensureInterface() error {
	// Ask the OS for the list of all network interfaces
	adapters, err := winipcfg.GetAdaptersAddresses(
		windows.AF_UNSPEC,                       // Address family (IPv4, IPv6)
		windows.GAA_FLAG_INCLUDE_ALL_INTERFACES, // return absolutely all adapters
	)
	if err != nil {
		return fmt.Errorf("failed to get network adapters: %w", err)
	}

	var tunGUID, physGUID *windows.GUID

	// Find the GUIDs by iterating over the adapter names

	for _, adapter := range adapters {
		friendlyName := adapter.FriendlyName()

		if friendlyName == w.tunName {
			// Convert the string AdapterName (which is a GUID on Windows) to *windows.GUID
			guid, err := windows.GUIDFromString(adapter.AdapterName())
			if err == nil {
				tunGUID = &guid
			}
		}

		if friendlyName == w.physName {
			guid, err := windows.GUIDFromString(adapter.AdapterName())
			if err == nil {
				physGUID = &guid
			}
		}
	}

	// Verify that both interfaces were found
	if tunGUID == nil {
		err = fmt.Errorf("tunnel interface '%s' not found", w.tunName)
		return err
	}
	if physGUID == nil {
		err = fmt.Errorf("physical interface '%s' not found", w.physName)
		return err
	}

	// GET THE LUID FOR OUR VPN TUNNEL AND THE PHYSICAL INTERFACE
	w.tunLuid, err = winipcfg.LUIDFromGUID(tunGUID)
	if err != nil {
		err = fmt.Errorf("failed to get tunnel interface LUID: %w", err)
		return err
	}

	w.physLuid, err = winipcfg.LUIDFromGUID(physGUID)
	if err != nil {
		err = fmt.Errorf("failed to get physical interface LUID: %w", err)
		return err
	}
	return err
}

// The constructor only takes ordinary strings! No Windows-specific details outside.
func newOSManager(remoteIPStr, gatewayStr, tunName, physName string) (tunManager, error) {
	remoteIP, err := netip.ParseAddr(remoteIPStr)
	if err != nil {
		return nil, fmt.Errorf("invalid remote IP: %w", err)
	}

	gateway, err := netip.ParseAddr(gatewayStr)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway IP: %w", err)
	}

	return &windowsManager{
		remoteIP: remoteIP,
		gateway:  gateway,
		tunName:  tunName,
		physName: physName,
	}, nil
}

func (m *windowsManager) SetupInterface(dnsStr string) error {

	// 1. Get the LUIDs of the physical and virtual interfaces
	if err := m.ensureInterface(); err != nil {
		return fmt.Errorf("resolve interfaces: %w", err)
	}

	// 2. ASSIGN AN IP ADDRESS TO THE TUNNEL
	tunIP := netip.MustParsePrefix("10.0.0.2/24")
	if err := m.tunLuid.SetIPAddresses([]netip.Prefix{tunIP}); err != nil {
		return fmt.Errorf("failed to set IP address to tunnel: %w", err)
	}

	// 3. ADD A ROUTE TO THE PROXY SERVER
	proxyPrefix := netip.PrefixFrom(m.remoteIP, 32)

	if err := m.physLuid.AddRoute(proxyPrefix, m.gateway, 0); err != nil {
		return fmt.Errorf("failed to add proxy route: %w", err)
	}

	// 4. ROUTE ALL INTERNET TRAFFIC THROUGH THE TUNNEL (0.0.0.0/1 and 128.0.0.0/1)
	// Send these routes via the TUN LUID. An empty IP (netip.Addr{}) is passed as
	// nextHop, because no gateway inside the tunnel is needed for a point-to-point
	// (TUN) interface.
	net1 := netip.MustParsePrefix("0.0.0.0/1")
	net2 := netip.MustParsePrefix("128.0.0.0/1")
	tunGateway := tunIP.Addr()

	if err := m.tunLuid.AddRoute(net1, tunGateway, 0); err != nil {
		return fmt.Errorf("failed to add 0.0.0.0/1 route: %w", err)
	}

	if err := m.tunLuid.AddRoute(net2, tunGateway, 0); err != nil {
		return fmt.Errorf("failed to add 128.0.0.0/1 route: %w", err)
	}

	// 5. SET DNS FOR THE VPN ADAPTER
	dnsServer := netip.MustParseAddr(dnsStr)
	if err := m.tunLuid.SetDNS(windows.AF_INET, []netip.Addr{dnsServer}, nil); err != nil {
		return fmt.Errorf("failed to set DNS: %w", err)
	}

	return nil
}

func (m *windowsManager) Teardown() error {
	proxyPrefix := netip.PrefixFrom(m.remoteIP, 32)
	return m.physLuid.DeleteRoute(proxyPrefix, m.gateway)
}
